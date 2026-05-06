package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/cache"
	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/gateway"
	"nvidia-api-gateway/pkg/middleware"
	"nvidia-api-gateway/pkg/prober"
	"nvidia-api-gateway/pkg/scheduler"
	"nvidia-api-gateway/pkg/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

const (
	defaultFrontendPort = "14000"
)

func main() {
	_ = godotenv.Load()

	if len(os.Args) > 1 && os.Args[1] == "serve-all" {
		serveAll()
		return
	}

	serveBackend()
}

func serveAll() {
	backendPort := resolveBackendPort()
	frontendPort := resolveFrontendPort()
	repoRoot, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to resolve working directory: %v", err)
	}
	frontendDir := filepath.Join(repoRoot, "frontend")

	if err := waitForPortFree(backendPort); err != nil {
		log.Fatalf("backend port %s unavailable: %v", backendPort, err)
	}
	if err := waitForPortFree(frontendPort); err != nil {
		log.Fatalf("frontend port %s unavailable: %v", frontendPort, err)
	}

	log.Printf("Backend starting on http://localhost:%s", backendPort)
	go serveBackend()

	if err := waitForPortReady(backendPort, 10*time.Second); err != nil {
		log.Fatalf("backend failed to start on port %s: %v", backendPort, err)
	}

	frontendCmd, err := startFrontend(frontendDir, backendPort)
	if err != nil {
		log.Fatalf("failed to start frontend: %v", err)
	}
	defer stopProcess(frontendCmd)

	log.Printf("Frontend starting on http://localhost:%s", frontendPort)
	waitForProcess(frontendCmd)
}

func serveBackend() {
	db.InitDB("gateway.json")

	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	var redisClient *redis.Client
	if redisURL != "" {
		candidate := redis.NewClient(&redis.Options{Addr: redisURL})
		if err := candidate.Ping(context.Background()).Err(); err != nil {
			log.Printf("Redis unavailable at %s; falling back to local mode: %v", redisURL, err)
			_ = candidate.Close()
		} else {
			redisClient = candidate
		}
	} else {
		log.Printf("REDIS_URL not set; using local in-memory mode")
	}

	sched := scheduler.NewScheduler(redisClient)
	if err := gateway.RestoreRecoverableStatuses(context.Background(), sched); err != nil {
		log.Printf("initial status restore skipped: %v", err)
		if loadErr := gateway.LoadActiveKeys(context.Background(), sched); loadErr != nil {
			log.Printf("initial key load skipped: %v", loadErr)
		}
	}
	go gateway.StartSchedulerRefresher(context.Background(), sched, 5*time.Minute)

	semanticCache := cache.NewSemanticCache(redisClient)
	usageTracker := middleware.NewUsageTracker(redisClient)
	gw := gateway.NewGateway(sched, semanticCache, usageTracker)

	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	pr := prober.NewProber(redisClient, sched)
	go pr.Start(context.Background())

	admin := app.Group("/admin")
	admin.Post("/keys", gateway.AddAPIKey(sched))
	admin.Post("/keys/import", gateway.ImportAPIKeys(sched))
	admin.Put("/keys/proxy", gateway.BindAPIKeysProxy(sched))
	admin.Get("/keys", gateway.GetAPIKeys)
	admin.Get("/proxies", gateway.GetUpstreamProxies)
	admin.Post("/proxies", gateway.AddUpstreamProxy(sched))
	admin.Put("/proxies/:id", gateway.UpdateUpstreamProxy(sched))
	admin.Patch("/proxies/:id/status", gateway.UpdateUpstreamProxyStatus(sched))
	admin.Delete("/proxies/:id", gateway.DeleteUpstreamProxy(sched))
	admin.Post("/proxies/test", gateway.TestUpstreamProxy)
	admin.Put("/keys/:id", gateway.UpdateAPIKey(sched))
	admin.Delete("/keys/:id", gateway.DeleteAPIKey(sched))
	admin.Patch("/keys/:id/status", gateway.UpdateAPIKeyStatus(sched))
	admin.Post("/keys/:id/probe", gateway.ProbeAPIKey(sched))
	admin.Get("/master-keys", gateway.GetMasterKeys)
	admin.Post("/master-keys", gateway.AddMasterKey)
	admin.Put("/master-keys/:id", gateway.UpdateMasterKey)
	admin.Delete("/master-keys/:id", gateway.DeleteMasterKey)
	admin.Patch("/master-keys/:id/status", gateway.UpdateMasterKeyStatus)
	admin.Post("/master-keys/:id/reveal", gateway.RevealMasterKey)
	admin.Post("/master-keys/:id/rotate", gateway.RotateMasterKey)
	admin.Post("/system/reload", gateway.ReloadSystem(sched))
	admin.Get("/system/stats", gateway.SchedulerStats(sched))
	admin.Get("/system/config", gateway.GetSystemConfig)
	admin.Put("/system/config", gateway.UpdateSystemConfig(sched))
	admin.Get("/upstream/models", gateway.GetUpstreamModels())
	admin.Get("/upstream/runtime", gateway.GetUpstreamRuntime(sched))
	admin.Get("/health/report", gateway.GetHealthReport(sched))
	admin.Post("/health/report/run", gateway.RunHealthReport(sched))

	protected := app.Group("/", middleware.MasterAuthMiddleware())
	protected.Get("/v1/models", gw.HandleOpenAIModels)
	protected.Get("/v1/models/:modelId", gw.HandleOpenAIModel)
	protected.Post("/v1/chat/completions", gw.HandleChatCompletions)
	protected.Post("/v1/embeddings", gw.HandleOpenAIEmbeddings)
	protected.Post("/v1/responses", gw.HandleOpenAIResponses)
	protected.Get("/v1/responses/:responseId", gw.GetOpenAIResponseByID)
	protected.Get("/anthropic/v1/models", gw.HandleClaudeModels)
	protected.Post("/anthropic/v1/messages", gw.HandleClaudeMessages)
	protected.Post("/v1/messages", gw.HandleClaudeMessages)
	protected.Post("/messages", gw.HandleClaudeMessages)
	protected.Post("/v1beta/models/:target", gw.HandleGeminiContent)
	protected.Post("/v1/models/:target", gw.HandleGeminiContent)

	port := resolveBackendPort()
	log.Printf("Starting Nvidia API Gateway on port %s", port)
	log.Fatal(app.Listen(":" + port))
}

func resolveBackendPort() string {
	return utils.ResolveBackendPort()
}

func resolveFrontendPort() string {
	port := os.Getenv("FRONTEND_PORT")
	if port == "" {
		port = defaultFrontendPort
	}
	return port
}

func startFrontend(frontendDir, backendPort string) (*exec.Cmd, error) {
	frontendPort := resolveFrontendPort()
	env := append(os.Environ(),
		"API_BASE_URL=http://localhost:"+backendPort,
		"FRONTEND_PORT="+frontendPort,
	)

	if err := ensureFrontendDependencies(frontendDir, env); err != nil {
		return nil, err
	}

	cmd := npmCommand(frontendDir, env, "run", "dev")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func ensureFrontendDependencies(frontendDir string, env []string) error {
	nextBinary := filepath.Join(frontendDir, "node_modules", ".bin", nextBinaryName())
	if _, err := os.Stat(nextBinary); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	log.Printf("frontend dependencies missing, running npm install in %s", frontendDir)
	cmd := npmCommand(frontendDir, env, "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install failed: %w", err)
	}

	if _, err := os.Stat(nextBinary); err != nil {
		return fmt.Errorf("frontend dependencies installed but %s is still missing: %w", nextBinary, err)
	}
	return nil
}

func npmCommand(dir string, env []string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		commandArgs := append([]string{"/c", "npm"}, args...)
		cmd = exec.Command("cmd", commandArgs...)
	} else {
		cmd = exec.Command("npm", args...)
	}
	cmd.Dir = dir
	cmd.Env = env
	return cmd
}

func nextBinaryName() string {
	if runtime.GOOS == "windows" {
		return "next.cmd"
	}
	return "next"
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func waitForProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if err := cmd.Wait(); err != nil {
		log.Fatalf("frontend process exited: %v", err)
	}
}

func waitForPortFree(port string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return err
	}
	_ = listener.Close()
	return nil
}

func waitForPortReady(port string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", port)
}
