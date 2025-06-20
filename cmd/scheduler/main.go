package main

import (
	"kronos-scheduler/pkg/energyplugin"
	"os"

	"k8s.io/component-base/logs"
	schedulerapp "k8s.io/kubernetes/cmd/kube-scheduler/app"
)

// Version is printed in logs; bump each release.
const Version = "v2.0.0-plugin"

func main() {
	// Build scheduler command with custom plugin
	command := schedulerapp.NewSchedulerCommand(
		schedulerapp.WithPlugin(energyplugin.Name, energyplugin.New ),
	)

	// init klog logs
	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
