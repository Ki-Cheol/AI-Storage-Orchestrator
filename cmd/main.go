package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ai-storage-orchestrator/pkg/apis"
	"ai-storage-orchestrator/pkg/controller"
	"ai-storage-orchestrator/pkg/k8s"
)

var (
	port       = flag.String("port", "8080", "HTTP server port")
	kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file (leave empty for in-cluster config)")
)

func main() {
	flag.Parse()

	log.Println("Starting AI Storage Orchestrator...")
	// Initialize Kubernetes client
	k8sClient, err := k8s.NewClient(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	log.Println("Kubernetes client initialized successfully")

	// Initialize migration controller
	migrationController := controller.NewMigrationController(k8sClient)
	log.Println("Migration controller initialized")

	// Initialize HTTP API handler
	apiHandler := apis.NewHandler(migrationController)
	router := apiHandler.SetupRoutes()
	
	log.Printf("HTTP server starting on port %s", *port)
	log.Println("Available endpoints:")
	log.Println("  POST /api/v1/migrations - Start new pod migration")
	log.Println("  GET  /api/v1/migrations/:id - Get migration details")
	log.Println("  GET  /api/v1/migrations/:id/status - Get migration status")
	log.Println("  GET  /api/v1/metrics - Get performance metrics")
	log.Println("  GET  /health - Health check")

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		if err := router.Run(":" + *port); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	log.Printf("AI Storage Orchestrator is ready to handle migration requests")

	// Wait for interrupt signal
	<-quit
	log.Println("Shutting down AI Storage Orchestrator...")
	log.Println("Graceful shutdown completed")
}