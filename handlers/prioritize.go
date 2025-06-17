package handlers

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	schedulerapi "k8s.io/kube-scheduler/extender/v1"
)

const (
	prometheusURL = "http://prometheus-kube-prometheus-prometheus.monitoring:9090"
	defaultEnergy = 100.0
)

// Prioritize handles the /prioritize extender endpoint
func Prioritize(c *fiber.Ctx) error {
	var args schedulerapi.ExtenderArgs
	if err := c.BodyParser(&args); err != nil {
		log.Printf("Failed to parse ExtenderArgs: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to decode request"})
	}

	jobType := args.Pod.Labels["jobType"]
	if jobType == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing jobType label"})
	}

	var priorityList []schedulerapi.HostPriority
	for _, node := range args.Nodes.Items {
		// Fetch energy in Watts for the node.
		energy, err := queryNodeEnergy(prometheusURL, node.Name)
		if err != nil {
			log.Printf("Energy query fallback for node %s: %v", node.Name, err)
		}

		// Normalise energy to 0–100 where 1000 W → 100.
		energyNorm := math.Min(energy/10.0, 100.0)

		// Existing queuing score (0–100, higher is better).
		queueScore := calculateScore(node, jobType)

		// Weight selection.
		var w1, w2 float64
		if jobType == "long" {
			w1, w2 = 0.6, 0.4
		} else {
			w1, w2 = 0.4, 0.6
		}

		finalScore := int64(w1*(100.0-energyNorm) + w2*float64(queueScore))
		// Clamp to scheduler score bounds.
		if finalScore < 0 {
			finalScore = 0
		}
		if finalScore > 100 {
			finalScore = 100
		}

		log.Printf("Node %s: energy=%.2f W (norm=%.1f), queueScore=%d, final=%d", node.Name, energy, energyNorm, queueScore, finalScore)

		priorityList = append(priorityList, schedulerapi.HostPriority{
			Host:  node.Name,
			Score: finalScore,
		})
	}

	return c.JSON(priorityList)
}

// queryNodeEnergy queries Prometheus for the power consumption (Watts) of a node using Kepler metrics.
// On any error it returns defaultEnergy and the error so that callers can log/handle as needed.
func queryNodeEnergy(url, node string) (float64, error) {
	client, err := api.NewClient(api.Config{Address: url})
	if err != nil {
		return defaultEnergy, fmt.Errorf("prometheus client init: %w", err)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := fmt.Sprintf(`rate(kepler_node_package_joules_total{node="%s"}[1m])`, node)
	result, warnings, err := v1api.Query(ctx, query, time.Now())
	if len(warnings) > 0 {
		log.Printf("prometheus warnings for node %s: %v", node, warnings)
	}
	if err != nil {
		return defaultEnergy, fmt.Errorf("prometheus query: %w", err)
	}

	vector, ok := result.(model.Vector)
	if !ok || len(vector) == 0 {
		return defaultEnergy, fmt.Errorf("no data returned for node %s", node)
	}

	// The rate gives Joules/second which is Watts.
	energy := float64(vector[0].Value)
	return energy, nil
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
