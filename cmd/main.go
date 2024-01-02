/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"github.com/eplightning/servicelb-xds/internal"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"net"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/eplightning/servicelb-xds/internal/controller"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var config internal.Config
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var preferIPv6 bool
	var nodeAddressType string
	var ingressStatus string

	var xdsAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&preferIPv6, "use-ipv6-endpoints", false, "Use IPv6 endpoints instead of IPv4")
	flag.StringVar(&config.LBClass, "load-balancer-class", "", "Load balancer class of services to manage")
	flag.BoolVar(&config.UseNodeAddresses, "use-node-addresses", false, "Use node addresses instead of pod addresses")
	flag.StringVar(&nodeAddressType, "node-address-type", "ExternalIP", "Which node addresses to use when use-node-addresses=true")
	flag.StringVar(&ingressStatus, "ingress-status", "", "Addresses to use for loadbalancer status")

	flag.StringVar(&xdsAddr, "xds-bind-address", ":50051", "The address the xDS endpoint binds to")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if preferIPv6 {
		config.AddressType = discoveryv1.AddressTypeIPv6
	} else {
		config.AddressType = discoveryv1.AddressTypeIPv4
	}

	config.NodeAddressType = v1.NodeAddressType(nodeAddressType)

	for _, addr := range strings.Split(ingressStatus, ",") {
		addrTrim := strings.TrimSpace(addr)

		if addrTrim != "" {
			ip := net.ParseIP(addrTrim)
			if ip != nil {
				config.IngressStatus = append(config.IngressStatus, internal.IngressStatusAddress{
					IP: ip,
				})
			} else {
				config.IngressStatus = append(config.IngressStatus, internal.IngressStatusAddress{
					Hostname: addrTrim,
				})
			}
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	graph := internal.NewServiceGraph()
	xds := internal.NewXDSServer(graph.GetCache(), internal.XDSOptions{
		Address: xdsAddr,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		Metrics:                       metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              "062fbd79.eplight.org",
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	svcReconciler := controller.NewServiceReconciler(
		mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("servicelb-xds"), graph, &config,
	)

	if err = svcReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Service")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	signalCtx := ctrl.SetupSignalHandler()
	cancelCtx, cancel := context.WithCancel(signalCtx)

	go func() {
		if err := xds.Serve(signalCtx); err != nil {
			setupLog.Error(err, "problem running xDS server")
		}

		cancel()
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(cancelCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
