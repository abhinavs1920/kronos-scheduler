package energyplugin

import (
	"context"
	"fmt"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Name is the plugin name used in the scheduler registry and config.
const Name = "EnergyAware"

// defaultEnergy is used when we fail to fetch power metrics.
const defaultEnergy = 100.0

// keplerEndpoint is the Prometheus URL that exposes Kepler metrics.
// Change if your deployment differs.
const keplerEndpoint = "http://kepler.kepler.svc.cluster.local:9102"

// Ensure the plugin implements the needed interfaces.
var _ framework.FilterPlugin = &EnergyAware{}
var _ framework.ScorePlugin = &EnergyAware{}
var _ framework.ScoreExtensions = &EnergyAware{}

// EnergyAware implements Filter and Score.
// For now Filter allows every node; Score prefers energy-efficient & lightly queued nodes.
type EnergyAware struct {
	handle framework.Handle
}

// New is the factory invoked by the scheduler.
func New(ctx context.Context,_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &EnergyAware{handle: h}, nil
}

// Name returns plugin name.
func (e *EnergyAware) Name() string { return Name }

// ------------------------- Filter ------------------------------
// Currently allow all nodes.
func (e *EnergyAware) Filter(_ context.Context, _ *framework.CycleState, _ *corev1.Pod, _ *framework.NodeInfo) *framework.Status {
	return framework.NewStatus(framework.Success)
}

// ------------------------- Score -------------------------------
// Score returns 0-100 (higher=better).
func (e *EnergyAware) Score(_ context.Context, _ *framework.CycleState, pod *corev1.Pod, nodeInfo *framework.NodeInfo) (int64, *framework.Status) {
	// We already have NodeInfo; derive the node name and proceed.
	nodeName := nodeInfo.Node().Name

	watts, err := queryNodeEnergy(nodeName)
	if err != nil {
		klog.V(4).InfoS("energy query failed", "node", nodeName, "err", err)
	}

	energyNorm := math.Min(watts/10.0, 100.0) // Normalize; e.g., 1000 W -> 100
	queueScore := calculateQueueScore(nodeInfo.Node(), pod.Labels["jobType"])

	energyPenalty := math.Log1p(watts) * 2.0
	wEnergy, wQueue := 0.5, 0.5
	if pod.Labels["jobType"] == "long" {
		wEnergy, wQueue = 0.3, 0.7
	}

	final := int64(wEnergy*(100-energyNorm) + wQueue*math.Max(0, float64(queueScore)-energyPenalty))
	if final < 0 {
		final = 0
	}
	if final > 100 {
		final = 100
	}
	return final, framework.NewStatus(framework.Success)
}

func (e *EnergyAware) NormalizeScore(_ context.Context, _ *framework.CycleState, _ *corev1.Pod, _ framework.NodeScoreList) *framework.Status {
	return framework.NewStatus(framework.Success)
}
func (e *EnergyAware) ScoreExtensions() framework.ScoreExtensions { return e }

// ------------------------ helpers ------------------------------

func queryNodeEnergy(node string) (float64, error) {
	client, err := promapi.NewClient(promapi.Config{Address: keplerEndpoint})
	if err != nil {
		return defaultEnergy, fmt.Errorf("prom client: %w", err)
	}
	api := promv1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := fmt.Sprintf(`rate(kepler_node_package_joules_total{node="%s"}[1m])`, node)
	res, _, err := api.Query(ctx, query, time.Now())
	if err != nil {
		return defaultEnergy, err
	}
	scalar, ok := res.(*model.Scalar)
	if !ok || scalar == nil {
		return defaultEnergy, fmt.Errorf("unexpected result type")
	}
	val := float64(scalar.Value)
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return defaultEnergy, fmt.Errorf("bad value")
	}
	return val, nil
}

func calculateQueueScore(node *corev1.Node, jobType string) int64 {
	cpu := node.Status.Allocatable.Cpu().MilliValue()
	rho := float64(cpu) / 1000.0
	lambda, varS := 0.1, 1.0
	if jobType == "long" {
		if rho >= 1.0 {
			return 10
		}
		wq := (lambda * varS) / (2 * (1 - rho))
		s := int64(100 - wq*10)
		if s < 0 {
			return 0
		}
		return s
	}
	s := int64(100 - rho*10)
	if s < 0 {
		return 0
	}
	return s
}
