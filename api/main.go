package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/handlers"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/services"
	"github.com/railpush/api/services/registrar"
)

func main() {
	godotenv.Load()
	cfg := config.Load()
	if err := validateCriticalConfig(cfg); err != nil {
		log.Fatalf("Invalid runtime configuration: %v", err)
	}

	if err := database.Connect(&cfg.Database); err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Migrations failed: %v", err)
	}

	// Create build directory
	os.MkdirAll(cfg.Docker.BuildPath, 0755)

	// Ensure Caddy dynamic server for subdomain routing
	caddyRouter := services.NewRouter(cfg)
	if !cfg.Deploy.DisableRouter && cfg.Deploy.Domain != "" && cfg.Deploy.Domain != "localhost" {
		if err := caddyRouter.EnsureDynamicServer(); err != nil {
			log.Printf("WARNING: Could not create Caddy dynamic server: %v (subdomain routing may not work)", err)
		}
	}

	// Start deploy worker
	worker := services.NewWorker(cfg)
	worker.Router = caddyRouter
	if cfg.Worker.Enabled {
		worker.Start(cfg.Worker.Concurrency)
	} else {
		log.Println("Deploy worker disabled (WORKER_ENABLED=false)")
	}
	autoscaler := services.NewAutoscaler(cfg, worker)
	autoscaler.Start()
	routeReconciler := services.NewRouteReconciler(cfg, caddyRouter)
	routeReconciler.Start()

	// Start scheduler for cron jobs
	scheduler := services.NewScheduler(cfg)
	scheduler.Start()

	// WebSocket handler (created early so we can wire build log broadcasting)
	wsH := handlers.NewWebSocketHandler(cfg)

	// Wire build log broadcasting from worker to WebSocket clients
	worker.OnBuildLog = func(deployID string, line string) {
		wsH.BroadcastBuildMessage(deployID, []byte(line))
	}

	r := mux.NewRouter()
	r.Use(middleware.CanonicalHostMiddleware(cfg))
	r.Use(middleware.CORSMiddleware(cfg))
	r.Use(middleware.RateLimitMiddleware(cfg))

	setupRoutes(r, cfg, worker, wsH)

	r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}).Methods("GET")

	r.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if database.DB == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db not initialized"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := database.DB.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}).Methods("GET")

	// Serve CLI binaries for download at /dl/railpush-{os}-{arch}
	r.PathPrefix("/dl/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		path := "./cli/" + name
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename="+name)
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, path)
	}).Methods("GET")

	r.PathPrefix("/").HandlerFunc(spaHandler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	// WriteTimeout is 0 (no timeout) because WebSocket connections are long-lived.
	// Individual HTTP handlers rely on context/request timeouts instead.
	srv := &http.Server{Addr: addr, Handler: r, ReadTimeout: 15 * time.Second, WriteTimeout: 0}

	go func() {
		log.Printf("RailPush API listening on %s", addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
	routeReconciler.Stop()
	autoscaler.Stop()
	scheduler.Stop()
	worker.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("Server stopped")
}

func validateCriticalConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("missing config")
	}
	if strings.TrimSpace(cfg.Database.Password) == "" {
		return fmt.Errorf("DB_PASSWORD is required")
	}
	if strings.TrimSpace(cfg.JWT.Secret) == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if strings.TrimSpace(cfg.Crypto.EncryptionKey) == "" {
		return fmt.Errorf("ENCRYPTION_KEY is required")
	}
	if len(strings.TrimSpace(cfg.Crypto.EncryptionKey)) < 32 {
		return fmt.Errorf("ENCRYPTION_KEY must be at least 32 characters")
	}
	return nil
}

func setupRoutes(r *mux.Router, cfg *config.Config, worker *services.Worker, wsH *handlers.WebSocketHandler) {
	stripeService := services.NewStripeService(cfg)
	auth := handlers.NewAuthHandler(cfg)
	svcH := handlers.NewServiceHandler(cfg, worker, stripeService)
	depH := handlers.NewDeployHandler(cfg, worker)
	envH := handlers.NewEnvVarHandler(cfg)
	diskH := handlers.NewDiskHandler()
	dbH := handlers.NewDatabaseHandler(cfg, worker, stripeService)
	kvH := handlers.NewKeyValueHandler(cfg, worker, stripeService)
	bpH := handlers.NewBlueprintHandler(cfg, worker)
	domH := handlers.NewDomainHandler(cfg, worker)
	rwH := handlers.NewRewriteRuleHandler(cfg, worker)
	logH := handlers.NewLogHandler(cfg, worker)
	whH := handlers.NewWebhookHandler(cfg, worker)
	metH := handlers.NewMetricsHandler(cfg)
	billH := handlers.NewBillingHandler(cfg, stripeService)
	projH := handlers.NewProjectHandler(cfg, worker, stripeService)
	workH := handlers.NewWorkspaceHandler()
	autoH := handlers.NewAutoscalingHandler()
	jobH := handlers.NewOneOffJobHandler(worker)
	prevH := handlers.NewPreviewEnvironmentHandler(cfg, worker)
	egH := handlers.NewEnvGroupHandler(cfg)
	samlH := handlers.NewSamlSSOHandler(cfg)
	aiFixH := handlers.NewAIFixHandler(cfg, worker)
	tplH := handlers.NewTemplateHandler(cfg, worker)
	verH := handlers.NewAPIVersionHandler()
	searchH := handlers.NewSearchHandler()
	rateLimitH := handlers.NewRateLimitHandler()
	regRouter := registrar.NewProviderRouter(cfg.Registrar)
	rdH := handlers.NewRegisteredDomainHandler(cfg, regRouter)

	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(middleware.APIVersionMiddleware())
	api.HandleFunc("/version", verH.GetVersion).Methods("GET")
	api.HandleFunc("/version/changelog", verH.GetVersionChangelog).Methods("GET")

	api.HandleFunc("/auth/register", auth.Register).Methods("POST")
	api.HandleFunc("/auth/login", auth.Login).Methods("POST")
	api.HandleFunc("/auth/verify", auth.VerifyEmail).Methods("POST")
	api.HandleFunc("/auth/verify/resend", auth.ResendVerification).Methods("POST")
	api.HandleFunc("/auth/logout", auth.Logout).Methods("POST")
	api.HandleFunc("/auth/github", auth.GitHubRedirect).Methods("GET")
	api.HandleFunc("/auth/github/callback", auth.GitHubCallback).Methods("GET")
	api.HandleFunc("/webhooks/github", whH.GitHubWebhook).Methods("POST")
	api.HandleFunc("/webhooks/stripe", billH.StripeWebhook).Methods("POST")
	api.HandleFunc("/webhooks/alertmanager", whH.AlertmanagerWebhook).Methods("POST")
	api.HandleFunc("/domains/search", rdH.SearchDomains).Methods("POST")
	api.HandleFunc("/workspaces/{id}/sso/saml/metadata", samlH.Metadata).Methods("GET")
	api.HandleFunc("/workspaces/{id}/sso/saml/acs", samlH.ACS).Methods("POST")

	authed := api.PathPrefix("").Subrouter()
	authed.Use(middleware.AuthMiddleware(cfg))

	authed.HandleFunc("/auth/user", auth.GetCurrentUser).Methods("GET")
	authed.HandleFunc("/rate-limit", rateLimitH.GetCurrent).Methods("GET")
	authed.HandleFunc("/settings/blueprint-ai", auth.GetBlueprintAISettings).Methods("GET")
	authed.HandleFunc("/settings/blueprint-ai", auth.UpdateBlueprintAISettings).Methods("PUT")
	authed.HandleFunc("/auth/api-keys", auth.ListAPIKeys).Methods("GET")
	authed.HandleFunc("/auth/api-keys", auth.CreateAPIKey).Methods("POST")
	authed.HandleFunc("/auth/api-keys/{id}", auth.DeleteAPIKey).Methods("DELETE")

	opsH := handlers.NewOpsIncidentsHandler(cfg)
	authed.HandleFunc("/ops/incidents", opsH.ListIncidents).Methods("GET")
	authed.HandleFunc("/ops/incidents/{id}", opsH.GetIncident).Methods("GET")
	authed.HandleFunc("/ops/incidents/{id}/ack", opsH.AcknowledgeIncident).Methods("POST")
	authed.HandleFunc("/ops/incidents/{id}/silence", opsH.SilenceIncident).Methods("POST")

	opsDashH := handlers.NewOpsDashboardHandler(cfg)
	authed.HandleFunc("/ops/overview", opsDashH.Overview).Methods("GET")
	authed.HandleFunc("/ops/users", opsDashH.ListUsers).Methods("GET")
	authed.HandleFunc("/ops/workspaces", opsDashH.ListWorkspaces).Methods("GET")
	authed.HandleFunc("/ops/services", opsDashH.ListServices).Methods("GET")
	authed.HandleFunc("/ops/deploys", opsDashH.ListDeploys).Methods("GET")
	authed.HandleFunc("/ops/email/outbox", opsDashH.ListEmailOutbox).Methods("GET")
	authed.HandleFunc("/ops/settings", opsDashH.Settings).Methods("GET")
	authed.HandleFunc("/ops/actions/auto-deploy/enable-all", opsDashH.EnableAutoDeployAll).Methods("POST")
	authed.HandleFunc("/ops/actions/email/test", opsDashH.SendTestEmail).Methods("POST")

	opsAdminH := handlers.NewOpsAdminHandler(cfg, worker)
	authed.HandleFunc("/ops/users/{id}", opsAdminH.UpdateUser).Methods("PATCH")
	authed.HandleFunc("/ops/users/{id}/suspend", opsAdminH.SuspendUser).Methods("POST")
	authed.HandleFunc("/ops/users/{id}/resume", opsAdminH.ResumeUser).Methods("POST")
	authed.HandleFunc("/ops/workspaces/{id}/suspend", opsAdminH.SuspendWorkspace).Methods("POST")
	authed.HandleFunc("/ops/workspaces/{id}/resume", opsAdminH.ResumeWorkspace).Methods("POST")
	authed.HandleFunc("/ops/services/{id}/restart", opsAdminH.RestartService).Methods("POST")
	authed.HandleFunc("/ops/services/{id}/suspend", opsAdminH.SuspendService).Methods("POST")
	authed.HandleFunc("/ops/services/{id}/resume", opsAdminH.ResumeService).Methods("POST")
	authed.HandleFunc("/ops/datastores", opsAdminH.ListDatastores).Methods("GET")
	authed.HandleFunc("/ops/audit-logs", opsAdminH.ListAuditLogs).Methods("GET")

	opsBillH := handlers.NewOpsBillingHandler(cfg)
	authed.HandleFunc("/ops/billing/customers", opsBillH.ListCustomers).Methods("GET")
	authed.HandleFunc("/ops/billing/customers/{id}", opsBillH.GetCustomer).Methods("GET")

	opsTicketsH := handlers.NewOpsTicketsHandler(cfg)
	authed.HandleFunc("/ops/tickets", opsTicketsH.ListTickets).Methods("GET")
	authed.HandleFunc("/ops/tickets/bulk", opsTicketsH.BulkUpdateTickets).Methods("POST")
	authed.HandleFunc("/ops/tickets/{id}", opsTicketsH.GetTicket).Methods("GET")
	authed.HandleFunc("/ops/tickets/{id}", opsTicketsH.UpdateTicket).Methods("PATCH")
	authed.HandleFunc("/ops/tickets/{id}/messages", opsTicketsH.CreateMessage).Methods("POST")

	opsCreditsH := handlers.NewOpsCreditsHandler(cfg, stripeService)
	authed.HandleFunc("/ops/credits/workspaces", opsCreditsH.ListWorkspaces).Methods("GET")
	authed.HandleFunc("/ops/credits/workspaces/{id}", opsCreditsH.GetWorkspace).Methods("GET")
	authed.HandleFunc("/ops/credits/workspaces/{id}/grant", opsCreditsH.Grant).Methods("POST")

	opsTechH := handlers.NewOpsTechnicalHandler(cfg)
	authed.HandleFunc("/ops/kube/summary", opsTechH.KubeSummary).Methods("GET")

	opsClusterH := handlers.NewOpsClusterHandler(cfg)
	authed.HandleFunc("/ops/cluster", opsClusterH.Summary).Methods("GET")

	opsPerfH := handlers.NewOpsPerformanceHandler(cfg)
	authed.HandleFunc("/ops/performance", opsPerfH.Summary).Methods("GET")

	ghH := handlers.NewGitHubHandler(cfg, services.NewGitHub(cfg))
	authed.HandleFunc("/github/repos", ghH.ListRepos).Methods("GET")
	authed.HandleFunc("/github/repos/{owner}/{repo}/branches", ghH.ListBranches).Methods("GET")
	authed.HandleFunc("/github/repos/{owner}/{repo}/workflows", ghH.ListWorkflows).Methods("GET")

	supportH := handlers.NewSupportHandler(cfg)
	authed.HandleFunc("/support/tickets", supportH.ListTickets).Methods("GET")
	authed.HandleFunc("/support/tickets", supportH.CreateTicket).Methods("POST")
	authed.HandleFunc("/support/tickets/{id}", supportH.GetTicket).Methods("GET")
	authed.HandleFunc("/support/tickets/{id}/tags", supportH.UpdateTicketTags).Methods("PATCH")
	authed.HandleFunc("/support/tickets/{id}/messages", supportH.CreateMessage).Methods("POST")
	authed.HandleFunc("/templates", tplH.ListTemplates).Methods("GET")
	authed.HandleFunc("/templates/{id}", tplH.GetTemplate).Methods("GET")
	authed.HandleFunc("/templates/{id}/deploy", tplH.DeployTemplate).Methods("POST")
	authed.HandleFunc("/search", searchH.Search).Methods("GET")

	authed.HandleFunc("/services", svcH.ListServices).Methods("GET")
	authed.HandleFunc("/services", svcH.CreateService).Methods("POST")
	authed.HandleFunc("/services/{id}", svcH.GetService).Methods("GET")
	authed.HandleFunc("/services/{id}/github/workflows", svcH.ListServiceGitHubWorkflows).Methods("GET")
	authed.HandleFunc("/services/{id}/github/webhook/status", svcH.GetServiceGitHubWebhookStatus).Methods("GET")
	authed.HandleFunc("/services/{id}/github/webhook/repair", svcH.RepairServiceGitHubWebhook).Methods("POST")
	authed.HandleFunc("/services/{id}/event-webhook", svcH.GetServiceEventWebhookConfig).Methods("GET")
	authed.HandleFunc("/services/{id}/event-webhook", svcH.UpdateServiceEventWebhookConfig).Methods("PUT")
	authed.HandleFunc("/services/{id}/event-webhook/test", svcH.TestServiceEventWebhook).Methods("POST")
	authed.HandleFunc("/services/{id}", svcH.UpdateService).Methods("PATCH")
	authed.HandleFunc("/services/{id}", svcH.DeleteService).Methods("DELETE")
	authed.HandleFunc("/services/{id}/restore", svcH.RestoreService).Methods("POST")
	authed.HandleFunc("/services/{id}/restart", svcH.RestartService).Methods("POST")
	authed.HandleFunc("/services/{id}/suspend", svcH.SuspendService).Methods("POST")
	authed.HandleFunc("/services/{id}/resume", svcH.ResumeService).Methods("POST")
	authed.HandleFunc("/services/{id}/autoscaling", autoH.GetPolicy).Methods("GET")
	authed.HandleFunc("/services/{id}/autoscaling", autoH.UpsertPolicy).Methods("PUT")
	authed.HandleFunc("/services/{id}/jobs", jobH.ListServiceJobs).Methods("GET")
	authed.HandleFunc("/services/{id}/jobs", jobH.RunServiceJob).Methods("POST")
	authed.HandleFunc("/jobs/{jobId}", jobH.GetJob).Methods("GET")

	authed.HandleFunc("/services/{id}/deploys", depH.TriggerDeploy).Methods("POST")
	authed.HandleFunc("/services/{id}/deploys", depH.ListDeploys).Methods("GET")
	authed.HandleFunc("/services/{id}/deploys/{deployId}", depH.GetDeploy).Methods("GET")
	authed.HandleFunc("/services/{id}/deploys/{deployId}/wait", depH.WaitDeploy).Methods("POST")
	authed.HandleFunc("/services/{id}/deploys/{deployId}/rollback", depH.Rollback).Methods("POST")
	authed.HandleFunc("/services/{id}/deploys/{deployId}/queue", depH.QueuePosition).Methods("GET")

	authed.HandleFunc("/services/{id}/ai-fix", aiFixH.StartFix).Methods("POST")
	authed.HandleFunc("/services/{id}/ai-fix/status", aiFixH.GetStatus).Methods("GET")

	authed.HandleFunc("/services/{id}/env-vars", envH.ListEnvVars).Methods("GET")
	authed.HandleFunc("/services/{id}/env-vars", envH.BulkUpdateEnvVars).Methods("PUT")
	authed.HandleFunc("/services/{id}/env-vars", envH.MergeEnvVars).Methods("PATCH")
	authed.HandleFunc("/services/{id}/disks", diskH.ListServiceDisks).Methods("GET")
	authed.HandleFunc("/services/{id}/disks", diskH.UpsertServiceDisk).Methods("PUT")
	authed.HandleFunc("/services/{id}/disks", diskH.DeleteServiceDisk).Methods("DELETE")

	authed.HandleFunc("/services/{id}/custom-domains", domH.AddCustomDomain).Methods("POST")
	authed.HandleFunc("/services/{id}/custom-domains", domH.ListCustomDomains).Methods("GET")
	authed.HandleFunc("/services/{id}/custom-domains/{domain}/verify", domH.VerifyCustomDomain).Methods("POST")
	authed.HandleFunc("/services/{id}/custom-domains/{domain}", domH.DeleteCustomDomain).Methods("DELETE")

	authed.HandleFunc("/services/{id}/rewrite-rules", rwH.AddRewriteRule).Methods("POST")
	authed.HandleFunc("/services/{id}/rewrite-rules", rwH.ListRewriteRules).Methods("GET")
	authed.HandleFunc("/services/{id}/rewrite-rules/{ruleId}", rwH.DeleteRewriteRule).Methods("DELETE")

	authed.HandleFunc("/services/{id}/logs", logH.QueryLogs).Methods("GET")
	authed.HandleFunc("/ops/services/{id}/logs", logH.QueryLogsOps).Methods("GET")
	authed.HandleFunc("/services/{id}/metrics", metH.GetServiceMetrics).Methods("GET")
	authed.HandleFunc("/services/{id}/metrics/history", metH.GetServiceMetricsHistory).Methods("GET")

	authed.HandleFunc("/databases", dbH.ListDatabases).Methods("GET")
	authed.HandleFunc("/databases", dbH.CreateDatabase).Methods("POST")
	authed.HandleFunc("/databases/{id}", dbH.GetDatabase).Methods("GET")
	authed.HandleFunc("/databases/{id}/credentials/reveal", dbH.RevealDatabaseCredentials).Methods("POST")
	authed.HandleFunc("/databases/{id}", dbH.UpdateDatabase).Methods("PATCH")
	authed.HandleFunc("/databases/{id}", dbH.DeleteDatabase).Methods("DELETE")
	authed.HandleFunc("/databases/{id}/restore", dbH.RestoreDatabase).Methods("POST")
	authed.HandleFunc("/databases/{id}/backups", dbH.ListBackups).Methods("GET")
	authed.HandleFunc("/databases/{id}/backups", dbH.TriggerBackup).Methods("POST")
	authed.HandleFunc("/databases/{id}/replicas", dbH.ListReplicas).Methods("GET")
	authed.HandleFunc("/databases/{id}/replicas", dbH.CreateReplica).Methods("POST")
	authed.HandleFunc("/databases/{id}/replicas/{replicaId}/promote", dbH.PromoteReplica).Methods("POST")
	authed.HandleFunc("/databases/{id}/ha/enable", dbH.EnableHA).Methods("POST")
	authed.HandleFunc("/services/{id}/link-database", dbH.LinkDatabaseToService).Methods("POST")
	authed.HandleFunc("/services/{id}/link-database/{databaseId}", dbH.UnlinkDatabaseFromService).Methods("DELETE")
	authed.HandleFunc("/services/{id}/linked-databases", dbH.ListServiceDatabaseLinks).Methods("GET")

	authed.HandleFunc("/keyvalue", kvH.ListKeyValues).Methods("GET")
	authed.HandleFunc("/keyvalue", kvH.CreateKeyValue).Methods("POST")
	authed.HandleFunc("/keyvalue/{id}", kvH.GetKeyValue).Methods("GET")
	authed.HandleFunc("/keyvalue/{id}/credentials/reveal", kvH.RevealKeyValueCredentials).Methods("POST")
	authed.HandleFunc("/keyvalue/{id}", kvH.UpdateKeyValue).Methods("PATCH")
	authed.HandleFunc("/keyvalue/{id}", kvH.DeleteKeyValue).Methods("DELETE")
	authed.HandleFunc("/keyvalue/{id}/restore", kvH.RestoreKeyValue).Methods("POST")

	authed.HandleFunc("/billing", billH.GetBillingOverview).Methods("GET")
	authed.HandleFunc("/billing/sync", billH.SyncBilling).Methods("POST")
	authed.HandleFunc("/billing/checkout-session", billH.CreateCheckoutSession).Methods("POST")
	authed.HandleFunc("/billing/payment-method", billH.GetPaymentMethod).Methods("GET")
	authed.HandleFunc("/billing/portal-session", billH.CreatePortalSession).Methods("POST")

	authed.HandleFunc("/domains", rdH.ListDomains).Methods("GET")
	authed.HandleFunc("/domains", rdH.RegisterDomain).Methods("POST")
	authed.HandleFunc("/domains/{id}", rdH.GetDomain).Methods("GET")
	authed.HandleFunc("/domains/{id}", rdH.UpdateDomain).Methods("PATCH")
	authed.HandleFunc("/domains/{id}", rdH.DeleteDomain).Methods("DELETE")
	authed.HandleFunc("/domains/{id}/renew", rdH.RenewDomain).Methods("POST")
	authed.HandleFunc("/domains/{id}/dns", rdH.ListDnsRecords).Methods("GET")
	authed.HandleFunc("/domains/{id}/dns", rdH.CreateDnsRecord).Methods("POST")
	authed.HandleFunc("/domains/{id}/dns/{recordId}", rdH.UpdateDnsRecord).Methods("PUT")
	authed.HandleFunc("/domains/{id}/dns/{recordId}", rdH.DeleteDnsRecord).Methods("DELETE")

	authed.HandleFunc("/blueprints", bpH.ListBlueprints).Methods("GET")
	authed.HandleFunc("/blueprints", bpH.CreateBlueprint).Methods("POST")
	authed.HandleFunc("/blueprints/{id}", bpH.GetBlueprint).Methods("GET")
	authed.HandleFunc("/blueprints/{id}", bpH.UpdateBlueprint).Methods("PATCH")
	authed.HandleFunc("/blueprints/{id}", bpH.DeleteBlueprint).Methods("DELETE")
	authed.HandleFunc("/blueprints/{id}/sync", bpH.SyncBlueprint).Methods("POST")
	authed.HandleFunc("/projects", projH.ListProjects).Methods("GET")
	authed.HandleFunc("/projects", projH.CreateProject).Methods("POST")
	authed.HandleFunc("/projects/{id}", projH.GetProject).Methods("GET")
	authed.HandleFunc("/projects/{id}", projH.UpdateProject).Methods("PATCH")
	authed.HandleFunc("/projects/{id}", projH.DeleteProject).Methods("DELETE")
	authed.HandleFunc("/project-folders", projH.ListProjectFolders).Methods("GET")
	authed.HandleFunc("/project-folders", projH.CreateProjectFolder).Methods("POST")
	authed.HandleFunc("/project-folders/{id}", projH.UpdateProjectFolder).Methods("PATCH")
	authed.HandleFunc("/project-folders/{id}", projH.DeleteProjectFolder).Methods("DELETE")
	authed.HandleFunc("/projects/{id}/environments", projH.ListProjectEnvironments).Methods("GET")
	authed.HandleFunc("/projects/{id}/environments", projH.CreateProjectEnvironment).Methods("POST")
	authed.HandleFunc("/environments/{id}", projH.UpdateEnvironment).Methods("PATCH")
	authed.HandleFunc("/environments/{id}", projH.DeleteEnvironment).Methods("DELETE")
	authed.HandleFunc("/env-groups", egH.ListEnvGroups).Methods("GET")
	authed.HandleFunc("/env-groups", egH.CreateEnvGroup).Methods("POST")
	authed.HandleFunc("/env-groups/{id}", egH.GetEnvGroup).Methods("GET")
	authed.HandleFunc("/env-groups/{id}", egH.UpdateEnvGroup).Methods("PATCH")
	authed.HandleFunc("/env-groups/{id}", egH.DeleteEnvGroup).Methods("DELETE")
	authed.HandleFunc("/env-groups/{id}/vars", egH.ListEnvGroupVars).Methods("GET")
	authed.HandleFunc("/env-groups/{id}/vars", egH.BulkUpdateEnvGroupVars).Methods("PUT")
	authed.HandleFunc("/env-groups/{id}/link", egH.LinkService).Methods("POST")
	authed.HandleFunc("/env-groups/{id}/link/{serviceId}", egH.UnlinkService).Methods("DELETE")
	authed.HandleFunc("/env-groups/{id}/services", egH.ListLinkedServices).Methods("GET")
	authed.HandleFunc("/preview-environments", prevH.ListPreviewEnvironments).Methods("GET")
	authed.HandleFunc("/preview-environments", prevH.CreatePreviewEnvironment).Methods("POST")
	authed.HandleFunc("/preview-environments/{id}", prevH.UpdatePreviewEnvironment).Methods("PATCH")
	authed.HandleFunc("/preview-environments/{id}", prevH.DeletePreviewEnvironment).Methods("DELETE")
	authed.HandleFunc("/workspaces/{id}/members", workH.ListMembers).Methods("GET")
	authed.HandleFunc("/workspaces/{id}/members", workH.AddMember).Methods("POST")
	authed.HandleFunc("/workspaces/{id}/members/{userId}", workH.UpdateMemberRole).Methods("PATCH")
	authed.HandleFunc("/workspaces/{id}/members/{userId}", workH.RemoveMember).Methods("DELETE")
	authed.HandleFunc("/workspaces/{id}/audit-logs", workH.ListAuditLogs).Methods("GET")
	authed.HandleFunc("/workspaces/{id}/audit-logs.csv", workH.ExportAuditLogsCSV).Methods("GET")
	authed.HandleFunc("/workspaces/{id}/sso/saml/config", samlH.GetConfig).Methods("GET")
	authed.HandleFunc("/workspaces/{id}/sso/saml/config", samlH.UpsertConfig).Methods("PUT")

	r.HandleFunc("/ws/logs/{serviceId}", wsH.HandleLogStream)
	r.HandleFunc("/ws/builds/{deployId}", wsH.HandleBuildStream)
	r.HandleFunc("/ws/events", wsH.HandleEventStream)
}

func spaHandler(w http.ResponseWriter, r *http.Request) {
	path := "./dashboard/dist" + r.URL.Path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, "./dashboard/dist/index.html")
		return
	}
	http.ServeFile(w, r, path)
}
