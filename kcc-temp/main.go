// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	goflag "flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof" // Needed to allow pprof server to accept requests
	"time"

	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/kccmanager"
	controllermetrics "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/metrics"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/gcp/profiler"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/k8s"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/krmtotf"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/leaderelection"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/logging"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/metrics"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/ready"

	flag "github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var logger = klog.Log.WithName("setup")

func main() {
	ctx := context.Background()

	stop := signals.SetupSignalHandler()

	var (
		prometheusScrapeEndpoint      string
		scopedNamespace               string
		userProjectOverride           bool
		billingProject                string
		enablePprof                   bool
		pprofPort                     int
		enableLeaderElection          bool
		leaseDuration                 int64
		renewDeadline                 int64
		retryPeriod                   int64
		leaderElectionReleaseOnCancel bool
		leaseBucket                   string
		projectId                     string
	)
	flag.StringVar(&prometheusScrapeEndpoint, "prometheus-scrape-endpoint", ":8888", "configure the Prometheus scrape endpoint; :8888 as default")
	flag.BoolVar(&controllermetrics.ResourceNameLabel, "resource-name-label", false, "option to enable the resource name label on some Prometheus metrics; false by default")
	flag.BoolVar(&userProjectOverride, "user-project-override", false, "option to use the resource project for preconditions, quota, and billing, instead of the project the credentials belong to; false by default")
	flag.StringVar(&billingProject, "billing-project", "", "project to use for preconditions, quota, and billing if --user-project-override is enabled; empty by default; if this is left empty but --user-project-override is enabled, the resource's project will be used")
	flag.StringVar(&scopedNamespace, "scoped-namespace", "", "scope controllers to only watch resources in the specified namespace; if unspecified, controllers will run in cluster scope")
	flag.BoolVar(&enablePprof, "enable-pprof", false, "Enable the pprof server.")
	flag.IntVar(&pprofPort, "pprof-port", 6060, "The port that the pprof server binds to if enabled.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Int64Var(&leaseDuration, "lease-duration", 15,
		"LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership. ")
	flag.Int64Var(&renewDeadline, "renew-deadline", 10,
		"RenewDeadline is the duration that the acting controlplane will retry refreshing leadership before giving up. ")
	flag.Int64Var(&retryPeriod, "retry-period", 4,
		"RetryPeriod is the duration the LeaderElector clients should wait between tries of actions. ")
	flag.BoolVar(&leaderElectionReleaseOnCancel, "leader-release-on-cancel", false,
		"LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily when the Manager ends. ")
	flag.StringVar(&leaseBucket, "lease-storage-bucket", "", "The Google Clous Storage bucket that holds the lease.")
	flag.StringVar(&projectId, "project-id", "", "Google project ID to which the bucke belong")
	profiler.AddFlag(flag.CommandLine)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	flag.Parse()

	// Discard everything logged onto the Go standard logger. We do this since
	// there are cases of Terraform logging sensitive data onto the Go standard
	// logger.
	log.SetOutput(ioutil.Discard)

	logging.SetupLogger()

	duration := time.Duration(leaseDuration) * time.Second
	retry := time.Duration(retryPeriod) * time.Second
	renew := time.Duration(renewDeadline) * time.Second
	leaseLock, _ := leaderelection.NewLeaseLock(leaderelection.Option{
		LeaderElection: true,
		ProjectID:      projectId,
		Bucket:         leaseBucket,
		Lease:          "lease-global",
	}, "Cluster-eu1")

	// Start pprof server if enabled
	if enablePprof {
		go func() {
			if err := http.ListenAndServe(fmt.Sprintf(":%d", pprofPort), nil); err != nil {
				logger.Error(err, "error while running pprof server")
			}
		}()
	}

	// Start Cloud Profiler agent if enabled
	if err := profiler.StartIfEnabled(); err != nil {
		logging.Fatal(err, "error starting Cloud Profiler agent")
	}

	// Get a config to talk to the apiserver
	restCfg, err := config.GetConfig()
	if err != nil {
		logging.Fatal(err, "fatal getting configuration from APIServer.")
	}

	logger.Info("Creating the manager")
	mgr, err := newManager(ctx, restCfg, scopedNamespace, userProjectOverride, billingProject, enableLeaderElection, duration, retry, renew, leaseLock)
	if err != nil {
		logging.Fatal(err, "error creating the manager")
	}

	// Register controller OpenCensus views
	logger.Info("Registering controller OpenCensus views.")
	if controllermetrics.ResourceNameLabel {
		if err = metrics.RegisterControllerOpenCensusViewsWithResourceNameLabel(); err != nil {
			logging.Fatal(err, "error registering controller OpenCensus views with resource name label.")
		}
	} else {
		if err = metrics.RegisterControllerOpenCensusViews(); err != nil {
			logging.Fatal(err, "error registering controller OpenCensus views.")
		}
	}

	// Connect to the bucket in Google storage to enable leader election
	if err := leaseLock.(*leaderelection.LeaseLock).StartGCSClient(ctx); err != nil {
		logging.Fatal(err, "unable to start the Google cloud storage client")
	}

	// Register the Prometheus exporter
	logger.Info("Registering the Prometheus exporter")
	if err = metrics.RegisterPrometheusExporter(prometheusScrapeEndpoint); err != nil {
		logging.Fatal(err, "error registering the Prometheus exporter.")
	}

	// Record the process start time which will be used by prometheus-to-sd sidecar
	if err = metrics.RecordProcessStartTime(); err != nil {
		logging.Fatal(err, "error recording the process start time.")
	}

	// Set up the HTTP server for the readiness probe
	logger.Info("Setting container as ready...")
	ready.SetContainerAsReady()
	logger.Info("Container is ready.")

	logger.Info("Starting the Cmd.")

	// Start the Cmd
	logging.Fatal(mgr.Start(stop), "error during manager execution.")
}

func newManager(ctx context.Context, restCfg *rest.Config, scopedNamespace string, userProjectOverride bool, billingProject string, enableLeaderElection bool, duration, retry, renew time.Duration, leaseLock resourcelock.Interface) (manager.Manager, error) {
	krmtotf.SetUserAgentForTerraformProvider()
	controllersCfg := kccmanager.Config{
		ManagerOptions: manager.Options{
			Namespace:                           scopedNamespace,
			LeaderElection:                      enableLeaderElection,
			LeaderElectionID:                    "04ab161a.600lyy.io",
			LeaseDuration:                       &duration,
			RetryPeriod:                         &retry,
			RenewDeadline:                       &renew,
			LeaderElectionResourceLockInterface: leaseLock,
		},
	}

	controllersCfg.UserProjectOverride = userProjectOverride
	controllersCfg.BillingProject = billingProject
	// TODO(b/320784855): StateIntoSpecDefaultValue and StateIntoSpecUserOverride values should come from the flags.
	controllersCfg.StateIntoSpecDefaultValue = k8s.StateIntoSpecDefaultValueV1Beta1
	mgr, err := kccmanager.New(ctx, restCfg, controllersCfg)
	if err != nil {
		return nil, fmt.Errorf("error creating manager: %w", err)
	}
	return mgr, nil
}
