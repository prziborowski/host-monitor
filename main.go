package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/slack-go/slack"
)

// Config holds the application configuration
type Config struct {
	Hosts []Host `json:"hosts"`
	Slack struct {
		APIKey    string `json:"api_key"`
		Channel   string `json:"channel"`
		Username  string `json:"username"`
		IconEmoji string `json:"icon_emoji"`
		MessageTS string `json:"message_ts"` // Will be populated after first message
	} `json:"slack"`
	Ping struct {
		TimeoutSeconds    int `json:"timeout_seconds"`
		MaxFailures       int `json:"max_failures"`
		CheckIntervalSecs int `json:"check_interval_seconds"`
	} `json:"ping"`
}

// Host represents a host to monitor
type Host struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// HostStatus tracks the status of each host
type HostStatus struct {
	Host     Host
	Failures int
	LastUp   bool
}

// AlertStatus tracks the overall alert status
type AlertStatus struct {
	Active      bool
	MessageTS   string
	LastUpdated time.Time
}

var (
	config         Config
	hostStatuses   = make(map[string]*HostStatus)
	statusMutex    sync.Mutex
	shutdownChan   = make(chan struct{})
	downHosts      = make(map[string]bool)
	downHostsMutex sync.Mutex
	alertStatus    = AlertStatus{}
	alertMutex     sync.Mutex
	slackClient    *slack.Client
)

func main() {
	log.Println("Starting Host Monitoring Application")

	// Load configuration
	log.Println("Loading configuration from config/hosts.json")
	if err := loadConfig("config/hosts.json"); err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	log.Printf("Successfully loaded configuration for %d hosts", len(config.Hosts))

	// Initialize Slack client
	slackClient = slack.New(config.Slack.APIKey)
	log.Println("Initialized Slack client")

	// Initialize host statuses
	log.Println("Initializing host status tracking")
	for _, host := range config.Hosts {
		hostStatuses[host.IP] = &HostStatus{
			Host:     host,
			Failures: 0,
			LastUp:   true,
		}
		log.Printf("Initialized monitoring for %s (%s)", host.Name, host.IP)
	}

	// Start monitoring
	log.Println("Starting host monitoring routine")
	go monitorHosts()

	// Print configuration summary
	printConfigSummary()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	log.Println("Application started. Press Ctrl+C to shutdown gracefully.")
	<-sigChan
	close(shutdownChan)
	log.Println("Shutting down gracefully...")
}

func loadConfig(filename string) error {
	log.Printf("Reading configuration file: %s", filename)
	file, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("Error reading config file: %v", err)
		return err
	}

	log.Println("Parsing JSON configuration")
	if err := json.Unmarshal(file, &config); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		return err
	}

	log.Println("Configuration loaded successfully")
	return nil
}

func printConfigSummary() {
	log.Println("\n=== Configuration Summary ===")
	log.Printf("Monitoring %d hosts", len(config.Hosts))
	log.Printf("Slack alerts will be sent to: %s", config.Slack.Channel)
	log.Printf("Ping timeout: %d seconds", config.Ping.TimeoutSeconds)
	log.Printf("Max consecutive failures before alert: %d", config.Ping.MaxFailures)
	log.Printf("Check interval: %d seconds", config.Ping.CheckIntervalSecs)
	log.Println("================================\n")
}

func monitorHosts() {
	ticker := time.NewTicker(time.Duration(config.Ping.CheckIntervalSecs) * time.Second)
	defer ticker.Stop()

	log.Println("Starting monitoring loop")

	for {
		select {
		case <-ticker.C:
			startTime := time.Now()
			log.Printf("Starting monitoring cycle at %s", startTime.Format(time.RFC3339))

			var wg sync.WaitGroup
			for _, host := range config.Hosts {
				wg.Add(1)
				go func(h Host) {
					defer wg.Done()
					checkHost(h)
				}(host)
			}
			wg.Wait()

			cycleDuration := time.Since(startTime)
			log.Printf("Completed monitoring cycle in %v", cycleDuration)

			// Send consolidated alert if needed
			sendConsolidatedAlert()
		case <-shutdownChan:
			log.Println("Monitoring loop shutting down")
			return
		}
	}
}

func checkHost(host Host) {
	log.Printf("Checking host: %s (%s)", host.Name, host.IP)

	startTime := time.Now()
	reachable := pingHost(host.IP)
	duration := time.Since(startTime)

	status := hostStatuses[host.IP]

	statusMutex.Lock()
	if reachable {
		log.Printf("✅ Host %s (%s) is reachable (ping took %v)", host.Name, host.IP, duration)
		status.Failures = 0
		status.LastUp = true
	} else {
		log.Printf("❌ Host %s (%s) is unreachable (ping took %v)", host.Name, host.IP, duration)
		status.Failures++
		status.LastUp = false
	}
	statusMutex.Unlock()

	log.Printf("Host %s (%s): failures=%d/%d, last_status=%v",
		host.Name, host.IP,
		status.Failures, config.Ping.MaxFailures,
		map[bool]string{true: "UP", false: "DOWN"}[reachable])
}

// PingResult represents the result of a ping operation
type PingResult struct {
	Success  bool
	Duration time.Duration
	Error    error
}

// pingHost uses the system's ping command for better reliability
func pingHost(ip string) bool {
	log.Printf("Pinging %s with timeout %d seconds", ip, config.Ping.TimeoutSeconds)

	startTime := time.Now()
	result := doPing(ip)
	duration := time.Since(startTime)

	if result.Success {
		log.Printf("Ping to %s succeeded in %v", ip, duration)
		return true
	}

	log.Printf("Ping to %s failed after %v: %v", ip, duration, result.Error)
	return false
}

// doPing executes the system's ping command
func doPing(ip string) PingResult {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("ping", "-n", "1", "-w", fmt.Sprintf("%d", config.Ping.TimeoutSeconds*1000), ip)
	case "darwin":
		cmd = exec.Command("ping", "-c", "1", "-W", fmt.Sprintf("%d", config.Ping.TimeoutSeconds), ip)
	default: // linux, freebsd, etc.
		cmd = exec.Command("ping", "-c", "1", "-W", fmt.Sprintf("%d", config.Ping.TimeoutSeconds), ip)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		return PingResult{
			Success:  false,
			Duration: 0,
			Error:    fmt.Errorf("ping failed: %v, stderr: %s", err, stderr.String()),
		}
	}

	// Parse output to get round-trip time
	output := out.String()
	if strings.Contains(output, "bytes from") {
		// Success - parse RTT
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "time=") {
				parts := strings.Split(line, "time=")
				if len(parts) > 1 {
					rttStr := strings.TrimSpace(parts[1])
					rttStr = strings.TrimSuffix(rttStr, " ms")
					rtt, err := time.ParseDuration(rttStr + "ms")
					if err == nil {
						return PingResult{
							Success:  true,
							Duration: rtt,
							Error:    nil,
						}
					}
				}
			}
		}
	}

	return PingResult{
		Success:  true,
		Duration: 0,
		Error:    errors.New("ping succeeded but couldn't parse RTT"),
	}
}

func sendConsolidatedAlert() {
	downHostsMutex.Lock()
	defer downHostsMutex.Unlock()

	log.Println("Evaluating hosts for alert conditions")

	// Check for new down hosts
	newDownHosts := make(map[string]bool)
	for ip, status := range hostStatuses {
		if !status.LastUp && status.Failures >= config.Ping.MaxFailures {
			newDownHosts[ip] = true
			log.Printf("Host %s (%s) meets alert criteria (failures=%d)",
				status.Host.Name, ip, status.Failures)
		}
	}

	// Determine which hosts to alert about
	var hostsToAlert []string
	for ip := range newDownHosts {
		if !downHosts[ip] {
			hostsToAlert = append(hostsToAlert, ip)
			log.Printf("New alert condition for host %s (%s)",
				hostStatuses[ip].Host.Name, ip)
		}
	}

	// Update down hosts tracking
	for ip := range newDownHosts {
		downHosts[ip] = true
		log.Printf("Marking host %s (%s) as down in tracking",
			hostStatuses[ip].Host.Name, ip)
	}

	// Remove hosts that are back up
	for ip := range downHosts {
		if hostStatuses[ip].LastUp {
			log.Printf("Host %s (%s) is back up, removing from down hosts list",
				hostStatuses[ip].Host.Name, ip)
			delete(downHosts, ip)
		}
	}

	// Send alert if there are new down hosts or if we need to update existing alert
	if len(hostsToAlert) > 0 || len(downHosts) > 0 {
		log.Printf("Preparing to send/update Slack alert for %d host(s)", len(downHosts))
		message := createSlackMessage(downHosts)
		go sendSlackMessage(message)
	} else {
		log.Println("No alert conditions detected")
	}
}

func createSlackMessage(downHosts map[string]bool) string {
	log.Printf("Creating Slack message for %d host(s)", len(downHosts))

	var buffer bytes.Buffer
	buffer.WriteString("⚠️ *Hosts Status Alert* ⚠️\n\n")

	if len(downHosts) > 0 {
		buffer.WriteString("*Current Issues:*\n")
		for ip := range downHosts {
			host := hostStatuses[ip].Host
			buffer.WriteString(fmt.Sprintf("• %s (%s) - %d consecutive failures\n",
				host.Name, host.IP, config.Ping.MaxFailures))
			log.Printf("Added host %s (%s) to Slack message", host.Name, host.IP)
		}
	} else {
		buffer.WriteString("*All hosts are currently operational.*\n")
	}

	buffer.WriteString("\n*Last updated:* " + time.Now().Format(time.RFC3339))

	log.Println("Slack message created successfully")
	return buffer.String()
}

func sendSlackMessage(message string) {
	log.Println("Sending/updating Slack message")

	alertMutex.Lock()
	defer alertMutex.Unlock()

	// Prepare message options
	options := []slack.MsgOption{
		slack.MsgOptionText(message, false),
		slack.MsgOptionAsUser(true),
		slack.MsgOptionUsername(config.Slack.Username),
		slack.MsgOptionIconEmoji(config.Slack.IconEmoji),
	}

	// If we have an existing message, update it; otherwise, post a new one
	var err error
	if alertStatus.Active && alertStatus.MessageTS != "" {
		log.Printf("Updating existing Slack message (TS: %s)", alertStatus.MessageTS)
		_, _, _, err = slackClient.UpdateMessage(
			config.Slack.Channel,
			alertStatus.MessageTS,
			options...,
		)
	} else {
		log.Println("Posting new Slack message")
		channel, timestamp, err := slackClient.PostMessage(
			config.Slack.Channel,
			options...,
		)
		if err == nil {
			log.Printf("Message posted to channel %s with timestamp %s", channel, timestamp)
			config.Slack.MessageTS = timestamp
			alertStatus.Active = true
			alertStatus.MessageTS = timestamp
			alertStatus.LastUpdated = time.Now()
		}
	}

	if err != nil {
		log.Printf("Error sending Slack message: %v", err)
		return
	}

	log.Println("Slack message sent/updated successfully")
}
