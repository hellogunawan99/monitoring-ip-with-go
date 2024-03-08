package main

import (
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
)

// IPMonitor represents a single IP address to monitor
type IPMonitor struct {
	IP        string
	Alias     string
	Count     int
	Total     int
	mu        sync.Mutex
	Successes []int
}

func main() {
	// Replace YOUR_BOT_TOKEN with the actual token of your Telegram bot
	botToken := "bot_token"

	// Replace CHAT_ID with the ID of the chat you want to send the message to
	chatID := "chat_id"

	// List of 100 IP addresses to monitor with their aliases
	ipMonitors := []IPMonitor{
		{IP: "8.8.8.8", Alias: "PT 01"},
		{IP: "1.1.1.1", Alias: "PT 02"},
		{IP: "facebook.com", Alias: "PT 03"},
		{IP: "gunawanwibisono.com", Alias: "PT 04"},
		{IP: "medium.com", Alias: "PT 05"},
		{IP: "tesla.com", Alias: "PT 06"},
		{IP: "10.0.10.2", Alias: "PT 07"},
		{IP: "73.9.12.0", Alias: "PT 08"},
		{IP: "28.17.0.23", Alias: "PT 09"},
		{IP: "192.168.18.10", Alias: "PT 10"},
		// Add more IP addresses and aliases as needed (up to 100)
	}

	// Check IP reachability every 5 seconds for an hour
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Channel to receive system signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for a signal or the ticker
	hourTicker := time.NewTicker(time.Hour)
	defer hourTicker.Stop()

	for {
		select {
		case <-signalChan:
			fmt.Println("Received signal, stopping tickers...")
			ticker.Stop()
			hourTicker.Stop()
			return
		case <-hourTicker.C:
			sendStatusReport(botToken, chatID, ipMonitors)
		case <-ticker.C:
			checkIPReachability(ipMonitors)
		}
	}
}

func checkIPReachability(ipMonitors []IPMonitor) {
	var wg sync.WaitGroup
	wg.Add(len(ipMonitors))

	for i := range ipMonitors {
		go func(monitor *IPMonitor) {
			defer wg.Done()
			percentage, _ := isReachable(monitor.IP)
			monitor.mu.Lock()
			monitor.Total++
			if percentage == 100.0 {
				monitor.Count++
				monitor.Successes = append(monitor.Successes, 1)
			} else {
				monitor.Successes = append(monitor.Successes, 0)
			}
			monitor.mu.Unlock()
		}(&ipMonitors[i])
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

func sendStatusReport(botToken, chatID string, ipMonitors []IPMonitor) {
	var sb strings.Builder
	sb.WriteString("Persentase CPE connection/hour:\nping every 5 second\n\n")
	// Find the maximum length of the alias for proper alignment
	// maxAliasLength := 0
	// for _, monitor := range ipMonitors {
	// 	if len(monitor.Alias) > maxAliasLength {
	// 		maxAliasLength = len(monitor.Alias)
	// 	}
	// }

	for i := range ipMonitors {
		// monitor.mu.Lock()
		monitor := &ipMonitors[i] // Access the monitor using a reference
		overallPercentage := float64(monitor.Count) / float64(monitor.Total) * 100
		// to show the successes 1 and 0
		// successes := monitor.Successes // Use the monitor's specific successes list
		monitor.Count = 0
		monitor.Total = 0
		// No need to reset successes here (already in monitor object)
		monitor.mu.Unlock()
		// Calculate the number of spaces needed for alignment
		// numSpaces := maxAliasLength - len(monitor.Alias) + 1
		// padding := strings.Repeat(" ", numSpaces)

		sb.WriteString(fmt.Sprintf("%s   (%.2f%%)\n", monitor.Alias, overallPercentage))
		// to send the successes
		// sb.WriteString(fmt.Sprintf("%s%s(%.2f%%)\n", monitor.Alias, padding, overallPercentage))
		// sb.WriteString(fmt.Sprintf("Successes: %v\n", successes))
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
