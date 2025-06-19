package handlers

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	schedulerapi "k8s.io/kube-scheduler/extender/v1"
)

var (
	logger *zap.Logger
)

// InitLogger initializes the logger for the handlers package
func InitLogger(l *zap.Logger) {
	logger = l
}

const (
	keplerMetricsURL = "http://kepler.kepler.svc.cluster.local:9102/metrics"
	defaultEnergy   = 100.0
)

// Prioritize handles the /prioritize extender endpoint
func Prioritize(c *fiber.Ctx) error {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	sugar := logger.Sugar()

	sugar.Infow("Received prioritize request")

	var args schedulerapi.ExtenderArgs
	if err := c.BodyParser(&args); err != nil {
		sugar.Errorw("Failed to parse ExtenderArgs", "error", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to decode request"})
	}

	jobType := args.Pod.Labels["jobType"]
	sugar.Infow("Processing pod", "pod", args.Pod.Name, "jobType", jobType, "nodeCount", len(args.Nodes.Items))

	if jobType == "" {
		sugar.Warn("Pod missing jobType label")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing jobType label"})
	}

	var priorityList []schedulerapi.HostPriority
	for _, node := range args.Nodes.Items {
		sugar.Debugw("Processing node", "node", node.Name)

		// Fetch energy in Watts for the node.
		energy, err := queryNodeEnergy(keplerMetricsURL, node.Name)
		if err != nil {
			sugar.Warnw("Energy query fallback", "node", node.Name, "error", err)
		}

		// Normalise energy to 0–100 where 1000 W → 100.
		energyNorm := math.Min(energy/10.0, 100.0)
		sugar.Debugw("Node energy metrics", "node", node.Name, "watts", energy, "normalized", energyNorm)

		// Existing queuing score (0–100, higher is better).
		queueScore := calculateScore(node, jobType)
		sugar.Debugw("Node queue score", "node", node.Name, "score", queueScore)

		// Calculate energy penalty (higher energy usage = higher penalty)
		// We'll use a logarithmic scale to avoid overly aggressive penalties
		energyPenalty := math.Log1p(energy) * 2.0

		// Weight selection based on job type
		var w1, w2 float64
		if jobType == "long" {
			// For long jobs, prioritize queue model more (70%) and energy less (30%)
			w1, w2 = 0.3, 0.7
		} else {
			// For short jobs, balance between energy and queue model (50-50)
			w1, w2 = 0.5, 0.5
		}

		// Calculate final score with energy penalty
		adjustedQueueScore := math.Max(0, float64(queueScore)-energyPenalty)
		finalScore := int64(w1*(100.0-energyNorm) + w2*adjustedQueueScore)

		sugar.Debugw("Calculating final score",
			"node", node.Name,
			"energy", energy,
			"normalized", energyNorm,
			"queueScore", queueScore,
			"energyPenalty", energyPenalty,
			"adjustedQueueScore", adjustedQueueScore,
			"initialFinalScore", finalScore,
		)

		// Clamp to scheduler score bounds.
		if finalScore < 0 {
			finalScore = 0
		}
		if finalScore > 100 {
			finalScore = 100
		}

		sugar.Debugw("Node scoring complete",
			"node", node.Name,
			"energy", energy,
			"normalized", energyNorm,
			"queueScore", queueScore,
			"finalScore", finalScore,
		)

		priorityList = append(priorityList, schedulerapi.HostPriority{
			Host:  node.Name,
			Score: finalScore,
		})
	}

	return c.JSON(priorityList)
}

// queryNodeEnergy queries the Kepler metrics endpoint for the power consumption (Watts) of a node.
// On any error it returns defaultEnergy and the error so that callers can log/handle as needed.
func queryNodeEnergy(url, node string) (float64, error) {
	sugar := logger.Sugar()

	client, err := api.NewClient(api.Config{Address: "http://kepler.kepler.svc.cluster.local:9102"})
	if err != nil {
		sugar.Errorw("Error creating Prometheus client", "error", err)
		return defaultEnergy, fmt.Errorf("prometheus client init: %w", err)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Query the rate of energy consumption in Joules per second (Watts)
	query := fmt.Sprintf(`rate(kepler_node_package_joules_total{node="%s"}[1m])`, node)
	result, warnings, err := v1api.Query(ctx, query, time.Now())
	
	if len(warnings) > 0 {
		sugar.Warnw("Prometheus query warnings", "node", node, "warnings", warnings)
	}

	if err != nil {
		sugar.Errorw("Error querying Prometheus", "node", node, "error", err)
		return defaultEnergy, fmt.Errorf("prometheus query: %w", err)
	}

	scalar, ok := result.(*model.Scalar)
	if !ok || scalar == nil {
		sugar.Warnw("Unexpected or empty Prometheus result", "node", node, "result", result)
		return defaultEnergy, fmt.Errorf("unexpected result type or empty result for node %s", node)
	}

	watts := float64(scalar.Value)
	if math.IsNaN(watts) || math.IsInf(watts, 0) {
		sugar.Warnw("Invalid wattage value", "node", node, "watts", watts)
		return defaultEnergy, fmt.Errorf("invalid wattage value: %v", watts)
	}

	sugar.Debugw("Node energy consumption", "node", node, "watts", watts)
	return watts, nil
}

// calculateScore returns a node score (0–100) based on job type and simplified queue model
func calculateScore(node corev1.Node, jobType string) int64 {
	// Use Allocatable CPU as a proxy for system load (for demo purposes)
	cpuAlloc := node.Status.Allocatable.Cpu().MilliValue()
	rho := float64(cpuAlloc) / 1000.0 // Normalize to range like 0–2

	// Placeholder queue model values (can be dynamically tuned later)
	lambda := 0.1 // arrival rate
	varS := 1.0   // service time variance

	if jobType == "long" {
		// M/G/1: Wq = (λ × Var(S)) / (2 × (1 – ρ))
		if rho >= 1.0 {
			return 10 // prevent negative or undefined wait time when overloaded
		}
		wq := (lambda * varS) / (2 * (1 - rho))
		score := int64(100 - wq*10)
		if score < 0 {
			return 0
		}
		return score
	}

	// M/M/1 for short jobs → lighter penalty
	score := int64(100 - rho*10)
	if score < 0 {
		return 0
	}
	return score
}
