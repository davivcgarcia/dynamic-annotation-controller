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

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/controller"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/errors"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/resource"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/scheduler"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/security"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	mgr       manager.Manager

	// Test timeout and intervals
	timeout  = time.Second * 30
	interval = time.Millisecond * 250
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Test Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = schedulerv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("setting up the manager")
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		// Disable metrics server for tests
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	By("setting up the controller")
	setupController()

	By("starting the manager")
	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred(), "failed to run manager")
	}()

	// Wait for manager to be ready
	Eventually(func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}, timeout, interval).Should(BeTrue())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// setupController initializes and registers the AnnotationScheduleRule controller
func setupController() {
	setupControllerWithManager(mgr)
}

// setupControllerWithManager sets up the controller with a specific manager
func setupControllerWithManager(mgr manager.Manager) {
	// Create components
	resourceManager := resource.NewResourceManager(mgr.GetClient(), logf.Log, mgr.GetScheme())
	errorHandler := errors.NewErrorHandler(errors.DefaultRetryConfig(), logf.Log)
	recoveryManager := errors.NewStateRecoveryManager(mgr.GetClient(), logf.Log, errorHandler)
	securityValidator := security.NewAnnotationValidator()
	auditLogger := security.NewAuditLogger(mgr.GetClient(), logf.Log, mgr.GetScheme())
	securityConfigLoader := security.NewSecurityConfigLoader(mgr.GetClient(), "default")

	// Create controller
	reconciler := &controller.AnnotationScheduleRuleReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		ResourceManager:      resourceManager,
		Log:                  logf.Log,
		Metrics:              &controller.ExecutionMetrics{},
		ErrorHandler:         errorHandler,
		RecoveryManager:      recoveryManager,
		SecurityValidator:    securityValidator,
		AuditLogger:          auditLogger,
		SecurityConfigLoader: securityConfigLoader,
	}

	// Create task executor
	taskExecutor := controller.NewTaskExecutor(mgr.GetClient(), resourceManager, logf.Log, reconciler)

	// Create scheduler with task executor
	timeScheduler := scheduler.NewTimeScheduler(taskExecutor, logf.Log)
	reconciler.Scheduler = timeScheduler

	// Register controller with manager
	err := ctrl.NewControllerManagedBy(mgr).
		For(&schedulerv1.AnnotationScheduleRule{}).
		Complete(reconciler)
	Expect(err).NotTo(HaveOccurred())

	// Start the scheduler
	go func() {
		defer GinkgoRecover()
		err := timeScheduler.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
