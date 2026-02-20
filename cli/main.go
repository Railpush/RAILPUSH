// RailPush CLI — command-line interface for the RailPush platform.
//
// Usage:
//
//	railpush login
//	railpush whoami
//	railpush services list
//	railpush services get <id>
//	railpush services create --name <n> --type <t> --repo <url>
//	railpush services delete <id>
//	railpush services restart <id>
//	railpush services suspend <id>
//	railpush services resume <id>
//	railpush deploy <service-id>
//	railpush deploys list <service-id>
//	railpush logs <service-id> [--tail <n>]
//	railpush env list <service-id>
//	railpush env set <service-id> KEY=VALUE [KEY=VALUE ...]
//	railpush blueprints list
//	railpush blueprints create --name <n> --repo <url>
//	railpush blueprints sync <id>
//	railpush blueprints delete <id>
//	railpush databases list
//	railpush databases create --name <n> --plan <p>
//	railpush databases get <id>
//	railpush databases delete <id>
//	railpush domains list <service-id>
//	railpush domains add <service-id> <domain>
//	railpush domains delete <service-id> <domain>

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const version = "0.1.0"

// ── Config / Auth ────────────────────────────────────────────

type cliConfig struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

func configDir() string {
	if d := os.Getenv("RAILPUSH_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "railpush")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func loadConfig() cliConfig {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cliConfig{}
	}
	var c cliConfig
	json.Unmarshal(data, &c)
	return c
}

func saveConfig(c cliConfig) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(configPath(), data, 0600)
}

func requireAuth() cliConfig {
	c := loadConfig()
	if c.Token == "" {
		fatal("Not logged in. Run: railpush login")
	}
	if c.Host == "" {
		fatal("No host configured. Run: railpush login")
	}
	return c
}

// ── HTTP helpers ─────────────────────────────────────────────

func apiRequest(method, url, token string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

func apiURL(cfg cliConfig, path string) string {
	host := strings.TrimRight(cfg.Host, "/")
	if !strings.HasPrefix(host, "http") {
		host = "https://" + host
	}
	return host + "/api/v1" + path
}

func decodeJSON(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

func checkStatus(resp *http.Response) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			fatal("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		fatal("API error (%d): %s", resp.StatusCode, string(body))
	}
}

// ── Output helpers ───────────────────────────────────────────

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func printTable(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, col := range row {
			if i < len(widths) && len(col) > widths[i] {
				widths[i] = len(col)
			}
		}
	}
	// Header
	for i, h := range headers {
		fmt.Printf("%-*s  ", widths[i], h)
	}
	fmt.Println()
	for i := range headers {
		fmt.Printf("%-*s  ", widths[i], strings.Repeat("-", widths[i]))
	}
	fmt.Println()
	// Rows
	for _, row := range rows {
		for i, col := range row {
			if i < len(widths) {
				fmt.Printf("%-*s  ", widths[i], col)
			}
		}
		fmt.Println()
	}
}

func prompt(label string) string {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	val, _ := reader.ReadString('\n')
	return strings.TrimSpace(val)
}

func promptSecret(label string) string {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	val, _ := reader.ReadString('\n')
	return strings.TrimSpace(val)
}

// ── Flag parsing helpers ─────────────────────────────────────

func getFlag(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return a[len(name)+1:]
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// positionalAfterFlags returns the first positional argument that isn't a --flag or its value.
func positionalAfterFlags(args []string) []string {
	var result []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			skip = true // skip the flag's value
			continue
		}
		result = append(result, a)
	}
	return result
}

// ── Commands ─────────────────────────────────────────────────

func cmdLogin(args []string) {
	host := getFlag(args, "--host")
	if host == "" {
		host = os.Getenv("RAILPUSH_HOST")
	}
	if host == "" {
		host = prompt("RailPush host (e.g. railpush.com): ")
	}
	if host == "" {
		fatal("Host is required")
	}

	email := getFlag(args, "--email")
	if email == "" {
		email = prompt("Email: ")
	}
	password := getFlag(args, "--password")
	if password == "" {
		password = promptSecret("Password: ")
	}

	cfg := cliConfig{Host: host}
	resp, err := apiRequest("POST", apiURL(cfg, "/auth/login"), "", map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		fatal("Login failed: %v", err)
	}
	checkStatus(resp)

	var result struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(resp, &result); err != nil {
		fatal("Decode login response: %v", err)
	}
	if result.Token == "" {
		fatal("No token in login response")
	}

	cfg.Token = result.Token
	if err := saveConfig(cfg); err != nil {
		fatal("Save config: %v", err)
	}
	fmt.Println("Logged in successfully. Token saved to", configPath())
}

func cmdWhoami() {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/auth/user"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var user map[string]interface{}
	decodeJSON(resp, &user)
	email, _ := user["email"].(string)
	name, _ := user["name"].(string)
	id, _ := user["id"].(string)
	fmt.Printf("User:  %s\n", name)
	fmt.Printf("Email: %s\n", email)
	fmt.Printf("ID:    %s\n", id)
	fmt.Printf("Host:  %s\n", cfg.Host)
}

func cmdLogout() {
	os.Remove(configPath())
	fmt.Println("Logged out. Config removed.")
}

// ── Services ─────────────────────────────────────────────────

func cmdServicesList() {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/services"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var services []map[string]interface{}
	decodeJSON(resp, &services)

	if len(services) == 0 {
		fmt.Println("No services found.")
		return
	}
	var rows [][]string
	for _, s := range services {
		id, _ := s["id"].(string)
		name, _ := s["name"].(string)
		stype, _ := s["type"].(string)
		status, _ := s["status"].(string)
		plan, _ := s["plan"].(string)
		rows = append(rows, []string{id, name, stype, status, plan})
	}
	printTable([]string{"ID", "NAME", "TYPE", "STATUS", "PLAN"}, rows)
}

func cmdServicesGet(id string) {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/services/"+id), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var svc map[string]interface{}
	decodeJSON(resp, &svc)
	printJSON(svc)
}

func cmdServicesCreate(args []string) {
	name := getFlag(args, "--name")
	stype := getFlag(args, "--type")
	repo := getFlag(args, "--repo")
	branch := getFlag(args, "--branch")
	plan := getFlag(args, "--plan")
	if name == "" {
		fatal("--name is required")
	}
	if stype == "" {
		stype = "web"
	}
	if plan == "" {
		plan = "starter"
	}
	body := map[string]string{
		"name": name,
		"type": stype,
		"plan": plan,
	}
	if repo != "" {
		body["repo"] = repo
	}
	if branch != "" {
		body["branch"] = branch
	}

	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/services"), cfg.Token, body)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var svc map[string]interface{}
	decodeJSON(resp, &svc)
	id, _ := svc["id"].(string)
	fmt.Printf("Service created: %s (ID: %s)\n", name, id)
}

func cmdServicesDelete(id string) {
	cfg := requireAuth()
	resp, err := apiRequest("DELETE", apiURL(cfg, "/services/"+id), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	fmt.Printf("Service %s deleted.\n", id)
}

func cmdServicesAction(id, action string) {
	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/services/"+id+"/"+action), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	fmt.Printf("Service %s: %s OK\n", id, action)
}

// ── Deploy ───────────────────────────────────────────────────

func cmdDeploy(serviceID string) {
	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/services/"+serviceID+"/deploys"), cfg.Token, map[string]string{})
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var deploy map[string]interface{}
	decodeJSON(resp, &deploy)
	id, _ := deploy["id"].(string)
	status, _ := deploy["status"].(string)
	fmt.Printf("Deploy triggered: %s (status: %s)\n", id, status)
}

func cmdDeploysList(serviceID string) {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/services/"+serviceID+"/deploys"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var deploys []map[string]interface{}
	decodeJSON(resp, &deploys)
	if len(deploys) == 0 {
		fmt.Println("No deploys found.")
		return
	}
	var rows [][]string
	for _, d := range deploys {
		id, _ := d["id"].(string)
		status, _ := d["status"].(string)
		created, _ := d["created_at"].(string)
		commit, _ := d["commit_sha"].(string)
		short := commit
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, []string{id, status, short, created})
	}
	printTable([]string{"ID", "STATUS", "COMMIT", "CREATED"}, rows)
}

// ── Logs ─────────────────────────────────────────────────────

func cmdLogs(serviceID string, args []string) {
	cfg := requireAuth()
	tail := getFlag(args, "--tail")
	url := apiURL(cfg, "/services/"+serviceID+"/logs")
	if tail != "" {
		url += "?limit=" + tail
	}
	resp, err := apiRequest("GET", url, cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var logs []map[string]interface{}
	if err := decodeJSON(resp, &logs); err != nil {
		fatal("Decode logs: %v", err)
	}
	for _, l := range logs {
		ts, _ := l["timestamp"].(string)
		msg, _ := l["message"].(string)
		if ts != "" {
			fmt.Printf("[%s] %s\n", ts, msg)
		} else {
			fmt.Println(msg)
		}
	}
}

// ── Env Vars ─────────────────────────────────────────────────

func cmdEnvList(serviceID string) {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/services/"+serviceID+"/env-vars"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var envs []map[string]interface{}
	decodeJSON(resp, &envs)
	if len(envs) == 0 {
		fmt.Println("No environment variables set.")
		return
	}
	var rows [][]string
	for _, e := range envs {
		key, _ := e["key"].(string)
		val, _ := e["value"].(string)
		rows = append(rows, []string{key, val})
	}
	printTable([]string{"KEY", "VALUE"}, rows)
}

func cmdEnvSet(serviceID string, pairs []string) {
	if len(pairs) == 0 {
		fatal("Provide KEY=VALUE pairs")
	}
	vars := []map[string]string{}
	for _, p := range pairs {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			fatal("Invalid format %q — expected KEY=VALUE", p)
		}
		vars = append(vars, map[string]string{"key": parts[0], "value": parts[1]})
	}
	cfg := requireAuth()
	resp, err := apiRequest("PUT", apiURL(cfg, "/services/"+serviceID+"/env-vars"), cfg.Token, vars)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	resp.Body.Close()
	fmt.Printf("Updated %d env var(s) for service %s.\n", len(vars), serviceID)
}

// ── Blueprints ───────────────────────────────────────────────

func cmdBlueprintsList() {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/blueprints"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var bps []map[string]interface{}
	decodeJSON(resp, &bps)
	if len(bps) == 0 {
		fmt.Println("No blueprints found.")
		return
	}
	var rows [][]string
	for _, b := range bps {
		id, _ := b["id"].(string)
		name, _ := b["name"].(string)
		repo, _ := b["repo"].(string)
		status, _ := b["last_sync_status"].(string)
		rows = append(rows, []string{id, name, repo, status})
	}
	printTable([]string{"ID", "NAME", "REPO", "SYNC STATUS"}, rows)
}

func cmdBlueprintsCreate(args []string) {
	name := getFlag(args, "--name")
	repo := getFlag(args, "--repo")
	branch := getFlag(args, "--branch")
	if name == "" {
		fatal("--name is required")
	}
	if repo == "" {
		fatal("--repo is required")
	}
	body := map[string]string{
		"name": name,
		"repo": repo,
	}
	if branch != "" {
		body["branch"] = branch
	}
	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/blueprints"), cfg.Token, body)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var bp map[string]interface{}
	decodeJSON(resp, &bp)
	id, _ := bp["id"].(string)
	fmt.Printf("Blueprint created: %s (ID: %s)\n", name, id)
}

func cmdBlueprintsSync(id string) {
	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/blueprints/"+id+"/sync"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	fmt.Printf("Blueprint %s sync triggered.\n", id)
}

func cmdBlueprintsDelete(id string) {
	cfg := requireAuth()
	resp, err := apiRequest("DELETE", apiURL(cfg, "/blueprints/"+id), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	fmt.Printf("Blueprint %s deleted.\n", id)
}

// ── Databases ────────────────────────────────────────────────

func cmdDatabasesList() {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/databases"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var dbs []map[string]interface{}
	decodeJSON(resp, &dbs)
	if len(dbs) == 0 {
		fmt.Println("No databases found.")
		return
	}
	var rows [][]string
	for _, d := range dbs {
		id, _ := d["id"].(string)
		name, _ := d["name"].(string)
		engine, _ := d["engine"].(string)
		plan, _ := d["plan"].(string)
		status, _ := d["status"].(string)
		rows = append(rows, []string{id, name, engine, plan, status})
	}
	printTable([]string{"ID", "NAME", "ENGINE", "PLAN", "STATUS"}, rows)
}

func cmdDatabasesGet(id string) {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/databases/"+id), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var db map[string]interface{}
	decodeJSON(resp, &db)
	printJSON(db)
}

func cmdDatabasesCreate(args []string) {
	name := getFlag(args, "--name")
	plan := getFlag(args, "--plan")
	engine := getFlag(args, "--engine")
	if name == "" {
		fatal("--name is required")
	}
	if plan == "" {
		plan = "starter"
	}
	if engine == "" {
		engine = "postgresql"
	}
	body := map[string]string{
		"name":   name,
		"plan":   plan,
		"engine": engine,
	}
	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/databases"), cfg.Token, body)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var db map[string]interface{}
	decodeJSON(resp, &db)
	id, _ := db["id"].(string)
	fmt.Printf("Database created: %s (ID: %s)\n", name, id)
}

func cmdDatabasesDelete(id string) {
	cfg := requireAuth()
	resp, err := apiRequest("DELETE", apiURL(cfg, "/databases/"+id), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	fmt.Printf("Database %s deleted.\n", id)
}

// ── Custom Domains ───────────────────────────────────────────

func cmdDomainsList(serviceID string) {
	cfg := requireAuth()
	resp, err := apiRequest("GET", apiURL(cfg, "/services/"+serviceID+"/custom-domains"), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	var domains []map[string]interface{}
	decodeJSON(resp, &domains)
	if len(domains) == 0 {
		fmt.Println("No custom domains.")
		return
	}
	var rows [][]string
	for _, d := range domains {
		domain, _ := d["domain"].(string)
		verified, _ := d["verified"].(bool)
		v := "no"
		if verified {
			v = "yes"
		}
		rows = append(rows, []string{domain, v})
	}
	printTable([]string{"DOMAIN", "VERIFIED"}, rows)
}

func cmdDomainsAdd(serviceID, domain string) {
	cfg := requireAuth()
	resp, err := apiRequest("POST", apiURL(cfg, "/services/"+serviceID+"/custom-domains"), cfg.Token, map[string]string{
		"domain": domain,
	})
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	resp.Body.Close()
	fmt.Printf("Domain %s added to service %s.\n", domain, serviceID)
}

func cmdDomainsDelete(serviceID, domain string) {
	cfg := requireAuth()
	resp, err := apiRequest("DELETE", apiURL(cfg, "/services/"+serviceID+"/custom-domains/"+domain), cfg.Token, nil)
	if err != nil {
		fatal("Request failed: %v", err)
	}
	checkStatus(resp)
	resp.Body.Close()
	fmt.Printf("Domain %s removed from service %s.\n", domain, serviceID)
}

// ── Main ─────────────────────────────────────────────────────

func usage() {
	fmt.Print(`RailPush CLI v` + version + `

Usage: railpush <command> [subcommand] [options]

Commands:
  login                          Authenticate with RailPush
  logout                         Remove stored credentials
  whoami                         Show current user

  services list                  List all services
  services get <id>              Show service details
  services create [flags]        Create a service (--name, --type, --repo, --branch, --plan)
  services delete <id>           Delete a service
  services restart <id>          Restart a service
  services suspend <id>          Suspend a service
  services resume <id>           Resume a service

  deploy <service-id>            Trigger a deploy
  deploys list <service-id>      List deploys for a service

  logs <service-id> [--tail N]   View service logs

  env list <service-id>          List environment variables
  env set <service-id> K=V ...   Set environment variables

  blueprints list                List blueprints
  blueprints create [flags]      Create a blueprint (--name, --repo, --branch)
  blueprints sync <id>           Trigger blueprint sync
  blueprints delete <id>         Delete a blueprint

  databases list                 List databases
  databases get <id>             Show database details
  databases create [flags]       Create a database (--name, --plan, --engine)
  databases delete <id>          Delete a database

  domains list <service-id>      List custom domains
  domains add <service-id> <d>   Add a custom domain
  domains delete <service-id> <d> Remove a custom domain

  version                        Print CLI version

Environment:
  RAILPUSH_HOST                  Default host (overrides saved config)
  RAILPUSH_TOKEN                 API token (overrides saved config)
  RAILPUSH_CONFIG_DIR            Config directory (default: ~/.config/railpush)
`)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(0)
	}

	// Allow RAILPUSH_TOKEN env to override stored config.
	if tok := os.Getenv("RAILPUSH_TOKEN"); tok != "" {
		c := loadConfig()
		c.Token = tok
		if h := os.Getenv("RAILPUSH_HOST"); h != "" {
			c.Host = h
		}
		saveConfig(c) // transient in-memory would be better, but for simplicity just warn
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "login":
		cmdLogin(rest)
	case "logout":
		cmdLogout()
	case "whoami":
		cmdWhoami()
	case "version", "--version", "-v":
		fmt.Println("railpush v" + version)

	case "services":
		if len(rest) == 0 {
			fatal("Usage: railpush services <list|get|create|delete|restart|suspend|resume>")
		}
		sub := rest[0]
		subRest := rest[1:]
		switch sub {
		case "list", "ls":
			cmdServicesList()
		case "get":
			if len(subRest) == 0 {
				fatal("Usage: railpush services get <id>")
			}
			cmdServicesGet(subRest[0])
		case "create":
			cmdServicesCreate(subRest)
		case "delete", "rm":
			if len(subRest) == 0 {
				fatal("Usage: railpush services delete <id>")
			}
			cmdServicesDelete(subRest[0])
		case "restart":
			if len(subRest) == 0 {
				fatal("Usage: railpush services restart <id>")
			}
			cmdServicesAction(subRest[0], "restart")
		case "suspend":
			if len(subRest) == 0 {
				fatal("Usage: railpush services suspend <id>")
			}
			cmdServicesAction(subRest[0], "suspend")
		case "resume":
			if len(subRest) == 0 {
				fatal("Usage: railpush services resume <id>")
			}
			cmdServicesAction(subRest[0], "resume")
		default:
			fatal("Unknown services subcommand: %s", sub)
		}

	case "deploy":
		if len(rest) == 0 {
			fatal("Usage: railpush deploy <service-id>")
		}
		cmdDeploy(rest[0])

	case "deploys":
		if len(rest) < 2 || rest[0] != "list" {
			fatal("Usage: railpush deploys list <service-id>")
		}
		cmdDeploysList(rest[1])

	case "logs":
		if len(rest) == 0 {
			fatal("Usage: railpush logs <service-id> [--tail N]")
		}
		cmdLogs(rest[0], rest[1:])

	case "env":
		if len(rest) == 0 {
			fatal("Usage: railpush env <list|set> <service-id> ...")
		}
		sub := rest[0]
		subRest := rest[1:]
		switch sub {
		case "list", "ls":
			if len(subRest) == 0 {
				fatal("Usage: railpush env list <service-id>")
			}
			cmdEnvList(subRest[0])
		case "set":
			if len(subRest) < 2 {
				fatal("Usage: railpush env set <service-id> KEY=VALUE ...")
			}
			cmdEnvSet(subRest[0], subRest[1:])
		default:
			fatal("Unknown env subcommand: %s", sub)
		}

	case "blueprints", "bp":
		if len(rest) == 0 {
			fatal("Usage: railpush blueprints <list|create|sync|delete>")
		}
		sub := rest[0]
		subRest := rest[1:]
		switch sub {
		case "list", "ls":
			cmdBlueprintsList()
		case "create":
			cmdBlueprintsCreate(subRest)
		case "sync":
			if len(subRest) == 0 {
				fatal("Usage: railpush blueprints sync <id>")
			}
			cmdBlueprintsSync(subRest[0])
		case "delete", "rm":
			if len(subRest) == 0 {
				fatal("Usage: railpush blueprints delete <id>")
			}
			cmdBlueprintsDelete(subRest[0])
		default:
			fatal("Unknown blueprints subcommand: %s", sub)
		}

	case "databases", "db":
		if len(rest) == 0 {
			fatal("Usage: railpush databases <list|get|create|delete>")
		}
		sub := rest[0]
		subRest := rest[1:]
		switch sub {
		case "list", "ls":
			cmdDatabasesList()
		case "get":
			if len(subRest) == 0 {
				fatal("Usage: railpush databases get <id>")
			}
			cmdDatabasesGet(subRest[0])
		case "create":
			cmdDatabasesCreate(subRest)
		case "delete", "rm":
			if len(subRest) == 0 {
				fatal("Usage: railpush databases delete <id>")
			}
			cmdDatabasesDelete(subRest[0])
		default:
			fatal("Unknown databases subcommand: %s", sub)
		}

	case "domains":
		if len(rest) == 0 {
			fatal("Usage: railpush domains <list|add|delete> <service-id> [domain]")
		}
		sub := rest[0]
		subRest := rest[1:]
		switch sub {
		case "list", "ls":
			if len(subRest) == 0 {
				fatal("Usage: railpush domains list <service-id>")
			}
			cmdDomainsList(subRest[0])
		case "add":
			if len(subRest) < 2 {
				fatal("Usage: railpush domains add <service-id> <domain>")
			}
			cmdDomainsAdd(subRest[0], subRest[1])
		case "delete", "rm":
			if len(subRest) < 2 {
				fatal("Usage: railpush domains delete <service-id> <domain>")
			}
			cmdDomainsDelete(subRest[0], subRest[1])
		default:
			fatal("Unknown domains subcommand: %s", sub)
		}

	case "help", "--help", "-h":
		usage()

	default:
		fatal("Unknown command: %s. Run 'railpush help' for usage.", cmd)
	}
}
