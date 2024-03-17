package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3" // Import SQLite driver
)

// IPMonitor represents a single IP address to monitor
type IPMonitor struct {
	IP    string
	Alias string
}

const (
	numGoroutinesPerIP = 10 // Number of goroutines to create per IP address
	pingInterval       = 5  // Interval in seconds for the ticker
	maxConcurrentPings = 20 // Maximum number of concurrent pings
)

func main() {
	// Replace YOUR_BOT_TOKEN with the actual token of your Telegram bot
	botToken := "7132166614:AAFYC8xocSMOx8il3ODvTLn0wGmjIPGsb5s"

	// Replace CHAT_ID with the ID of the chat you want to send the message to
	chatID := "-1002121962452"

	// List of IP addresses to monitor with their aliases
	ipMonitors := []IPMonitor{
		{IP: "8.8.8.8", Alias: "Google"},
		{IP: "gunawanwibisono.com", Alias: "Gunawan Web"},
		{IP: "medium.com", Alias: "Medium"},
		{IP: "tesla.com", Alias: "Tesla"},
		{IP: "10.0.10.2", Alias: "Dummy 1"},
		{IP: "73.9.12.0", Alias: "Dummy 2"},
	}

	// Create or open the SQLite database
	db, err := sql.Open("sqlite3", "ping_results.db")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Create the table if it doesn't exist
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS ping_results (ip TEXT, alias TEXT, result INTEGER, timestamp TEXT)")
	if err != nil {
		panic(err)
	}

	// Check IP reachability every pingInterval/numGoroutinesPerIP seconds
	ticker := time.NewTicker(time.Duration(pingInterval*1000/numGoroutinesPerIP) * time.Millisecond)
	defer ticker.Stop()

	// Channel to receive system signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for a signal or the ticker
	hourTicker := time.NewTicker(time.Hour)
	defer hourTicker.Stop()

	// Hourly data storage
	hourlyData := make(map[string][]int)

	// Initialize daily data storage
	dailyData := make(map[string][]int)

	// Track the start time for the daily data
	dailyStartTime := time.Now().Truncate(24 * time.Hour)

	// Rate-limiting channel
	pingChan := make(chan struct{}, maxConcurrentPings)

	for {
		select {
		case <-signalChan:
			fmt.Println("Received signal, stopping tickers...")
			ticker.Stop()
			hourTicker.Stop()
			return
		case <-hourTicker.C:
			sendHourlyReport(botToken, chatID, hourlyData)
			hourlyData = make(map[string][]int) // Reset hourly data
		case <-ticker.C:
			checkIPReachability(ipMonitors, db, hourlyData, dailyData, dailyStartTime, pingChan)
		}
	}
}

func checkIPReachability(ipMonitors []IPMonitor, db *sql.DB, hourlyData, dailyData map[string][]int, dailyStartTime time.Time, pingChan chan struct{}) {
	// Replace YOUR_BOT_TOKEN with the actual token of your Telegram bot
	botToken := "7132166614:AAFYC8xocSMOx8il3ODvTLn0wGmjIPGsb5s"

	// Replace CHAT_ID with the ID of the chat you want to send the message to
	chatID := "-1002121962452"
	var wg sync.WaitGroup
	wg.Add(len(ipMonitors) * numGoroutinesPerIP)

	for _, monitor := range ipMonitors {
		for i := 0; i < numGoroutinesPerIP; i++ {
			go func(monitor IPMonitor) {
				defer wg.Done()
				pingChan <- struct{}{}
				percentage, _ := isReachable(monitor.IP)
				<-pingChan
				result := 0
				if percentage == 100.0 {
					result = 1
				}

				// Store the result in the SQLite database
				timestamp := time.Now().Format("2006-01-02 15:04:05")
				_, err := db.Exec("INSERT INTO ping_results (ip, alias, result, timestamp) VALUES (?, ?, ?, ?)", monitor.IP, monitor.Alias, result, timestamp)
				if err != nil {
					fmt.Println("Error storing result in database:", err)
				}

				// Update hourly data
				hourlyData[monitor.Alias] = append(hourlyData[monitor.Alias], result)

				// Update daily data
				if time.Now().After(dailyStartTime.Add(24 * time.Hour)) {
					// New day, reset daily data and update the daily start time
					dailyData = make(map[string][]int)
					dailyStartTime = time.Now().Truncate(24 * time.Hour)
				}
				dailyData[monitor.Alias] = append(dailyData[monitor.Alias], result)

				// Send daily report at 6 AM every day
				if time.Now().Hour() == 6 && time.Now().Minute() == 0 {
					sendDailyReport(botToken, chatID, dailyData)
					deletePingResultsFromDatabase(db)
					dailyData = make(map[string][]int) // Reset daily data
				}
			}(monitor)
		}
	}

	wg.Wait()
}

func isReachable(ip string) (float64, error) {
	cmd := exec.Command("ping", "-c", "1", ip)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}

	outputStr := string(output)
	re := regexp.MustCompile(`(\d+)% packet loss`)
	matches := re.FindStringSubmatch(outputStr)

	if len(matches) > 1 {
		packetLoss, _ := strconv.ParseFloat(matches[1], 64)
		return 100.0 - packetLoss, nil
	}

	return 0, fmt.Errorf("unable to parse ping output for %s", ip)
}

func sendHourlyReport(botToken, chatID string, hourlyData map[string][]int) {
	var sb strings.Builder
	sb.WriteString("Hourly Percentage CPE Connection:\n\n")

	for alias, results := range hourlyData {
		count := 0
		for _, result := range results {
			if result == 1 {
				count++
			}
		}
		overallPercentage := float64(count) / float64(len(results)) * 100
		sb.WriteString(fmt.Sprintf("%s (%.2f%%)\n", alias, overallPercentage))
	}

	message := sb.String()
	sendTelegramMessage(botToken, chatID, message)
}

func sendDailyReport(botToken, chatID string, dailyData map[string][]int) {
	var sb strings.Builder
	sb.WriteString("Daily Percentage CPE Connection:\n\n")

	for alias, results := range dailyData {
		count := 0
		for _, result := range results {
			if result == 1 {
				count++
			}
		}
		overallPercentage := float64(count) / float64(len(results)) * 100
		sb.WriteString(fmt.Sprintf("%s (%.2f%%)\n", alias, overallPercentage))
	}

	message := sb.String()
	sendTelegramMessage(botToken, chatID, message)
}

func sendTelegramMessage(botToken, chatID, message string) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	// Split the message into chunks of 4096 characters or less
	for i := 0; i < len(message); i += 4096 {
		end := i + 4096
		if end > len(message) {
			end = len(message)
		}
		chunk := message[i:end]

		// Construct payload for the chunk
		payload := url.Values{}
		payload.Add("chat_id", chatID)
		payload.Add("text", chunk)

		client := &http.Client{}
		resp, err := client.PostForm(apiURL, payload)
		if err != nil {
			fmt.Println("Error sending message to Telegram:", err)
			return
		}
		defer resp.Body.Close()

		// Debug logs
		fmt.Println("Telegram API Response Status:", resp.Status)
		// Read and print the response body for debugging purposes
		responseBody, _ := ioutil.ReadAll(resp.Body)
		fmt.Println("Telegram API Response Body:", string(responseBody))
	}
}

func deletePingResultsFromDatabase(db *sql.DB) {
	_, err := db.Exec("DELETE FROM ping_results")
	if err != nil {
		fmt.Println("Error deleting ping results from database:", err)
	}
}
