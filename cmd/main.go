/*
Copyright 2025.

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
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/controller"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/errors"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/resource"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/scheduler"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/security"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(schedulerv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// validateStartupConfiguration performs startup validation checks
func validateStartupConfiguration() error {
	// Validate required environment variables and configuration
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" && os.Getenv("KUBECONFIG") == "" {
		return fmt.Errorf("no Kubernetes configuration found: neither in-cluster config nor KUBECONFIG is available")
	}

	// Validate that we can create a Kubernetes client
	config := ctrl.GetConfigOrDie()
	if config == nil {
		return fmt.Errorf("failed to get Kubernetes configuration")
	}

	setupLog.Info("Startup validation completed successfully")
	return nil
}

// setupSignalHandler creates a context that is cancelled when a termination signal is received
func setupSignalHandler() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		setupLog.Info("Received termination signal, initiating graceful shutdown...")
		cancel()

		// Give a grace period for cleanup
		<-time.After(5 * time.Second)
		setupLog.Info("Grace period expired, forcing shutdown")
		os.Exit(1)
	}()

	return ctx
}

// createHealthChecks sets up comprehensive health and readiness checks
func createHealthChecks(mgr ctrl.Manager, reconciler *controller.AnnotationScheduleRuleReconciler) error {
	// Basic ping health check
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	// Custom readiness check that validates controller components
	readinessCheck := func(req *http.Request) error {
		// Check if scheduler is running
		if reconciler.Scheduler == nil {
			return fmt.Errorf("scheduler not initialized")
		}

		// Check if resource manager is available
		if reconciler.ResourceManager == nil {
			return fmt.Errorf("resource manager not initialized")
		}

		// Check if security validator is available
		if reconciler.SecurityValidator == nil {
			return fmt.Errorf("security validator not initialized")
		}

		return nil
	}

	if err := mgr.AddReadyzCheck("readyz", readinessCheck); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	return nil
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var leaderElectionNamespace string
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "",
		"Namespace where the leader election resource will be created. If not set, uses the namespace of the pod.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Perform startup validation
	if err := validateStartupConfiguration(); err != nil {
		setupLog.Error(err, "startup validation failed")
		os.Exit(1)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	// Configure manager options with enhanced leader election
	managerOptions := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "033b21d5.sandbox.davcgar.aws",
		// Enable graceful leader election handoff for faster transitions
		LeaderElectionReleaseOnCancel: true,
		// Configure leader election timing for high availability
		LeaseDuration: &[]time.Duration{15 * time.Second}[0],
		RenewDeadline: &[]time.Duration{10 * time.Second}[0],
		RetryPeriod:   &[]time.Duration{2 * time.Second}[0],
	}

	// Set leader election namespace if specified
	if leaderElectionNamespace != "" {
		managerOptions.LeaderElectionNamespace = leaderElectionNamespace
	}

	setupLog.Info("Creating controller manager",
		"leaderElection", enableLeaderElection,
		"leaderElectionNamespace", leaderElectionNamespace,
		"metricsAddr", metricsAddr,
		"probeAddr", probeAddr)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Setup signal handler for graceful shutdown
	ctx := setupSignalHandler()

	setupLog.Info("Initializing controller components...")

	// Initialize security components
	setupLog.Info("Loading security configuration...")
	securityConfigLoader := security.NewSecurityConfigLoader(mgr.GetClient(), "system")

	// Load security configuration and create validator
	securityValidator, err := securityConfigLoader.CreateValidatorFromConfig(ctx)
	if err != nil {
		setupLog.Error(err, "failed to load security configuration, using defaults")
		// Fall back to default security validator
		securityValidator = security.NewAnnotationValidator()
	}

	// Initialize audit logger
	auditLogger := security.NewAuditLogger(mgr.GetClient(), setupLog, mgr.GetScheme())

	// Initialize resource management components
	setupLog.Info("Initializing resource management components...")
	resourceManager := resource.NewResourceManager(mgr.GetClient(), setupLog, mgr.GetScheme())

	// Initialize error handling and recovery components
	setupLog.Info("Initializing error handling and recovery components...")
	errorHandler := errors.NewErrorHandler(nil, setupLog) // Use default retry config
	recoveryManager := errors.NewStateRecoveryManager(mgr.GetClient(), setupLog, errorHandler)

	// Initialize execution metrics
	metrics := &controller.ExecutionMetrics{}

	// Create task executor for the scheduler
	setupLog.Info("Initializing scheduler components...")
	// Will set reconciler later
	taskExecutor := controller.NewTaskExecutor(mgr.GetClient(), resourceManager, setupLog, nil)
	timeScheduler := scheduler.NewTimeScheduler(taskExecutor, setupLog)

	// Create the main reconciler with all components
	setupLog.Info("Creating main controller reconciler...")
	reconciler := &controller.AnnotationScheduleRuleReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Scheduler:            timeScheduler,
		ResourceManager:      resourceManager,
		Log:                  setupLog,
		Metrics:              metrics,
		ErrorHandler:         errorHandler,
		RecoveryManager:      recoveryManager,
		SecurityValidator:    securityValidator,
		AuditLogger:          auditLogger,
		SecurityConfigLoader: securityConfigLoader,
	}

	// Set the reconciler reference in the task executor (resolve circular dependency)
	taskExecutor.SetReconciler(reconciler)

	// Register the controller with the manager
	if err := reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AnnotationScheduleRule")
		os.Exit(1)
	}

	// Start performance optimization routines
	setupLog.Info("Starting performance optimization routines...")
	reconciler.StartPerformanceOptimizations(ctx)
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	// Setup comprehensive health and readiness checks
	setupLog.Info("Setting up health and readiness checks...")
	if err := createHealthChecks(mgr, reconciler); err != nil {
		setupLog.Error(err, "unable to set up health checks")
		os.Exit(1)
	}

	// Start the manager with graceful shutdown handling
	setupLog.Info("Starting controller manager...",
		"version", "v1.0.0",
		"leaderElection", enableLeaderElection)

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	setupLog.Info("Controller manager stopped gracefully")
}
