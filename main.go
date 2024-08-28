package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Config struct {
	RabbitMQ struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		Host           string `json:"host"`
		Port           string `json:"port"`
		ManagementPort string `json:"management_port"`
	} `json:"rabbitmq"`
}

type QueueInfo struct {
	Name          string `json:"name"`
	VHost         string `json:"vhost"`
	Type          string `json:"type"`
	State         string `json:"state"`
	Messages      int    `json:"messages"`
	MessagesReady int    `json:"messages_ready"`
	MessagesUnack int    `json:"messages_unacknowledged"`
	MessageStats  struct {
		Publish    int `json:"publish"`
		DeliverGet int `json:"deliver_get"`
		Ack        int `json:"ack"`
	} `json:"message_stats"`
}

var (
	lastAlertTime time.Time
	alertCooldown = 1 * time.Minute
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func loadConfig(filename string) (Config, error) {
	var config Config
	configFile, err := os.ReadFile(filename)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(configFile, &config)
	return config, err
}

func getQueues(config Config) ([]QueueInfo, error) {
	url := fmt.Sprintf("http://%s:%s/api/queues", config.RabbitMQ.Host, config.RabbitMQ.ManagementPort)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(config.RabbitMQ.Username, config.RabbitMQ.Password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var queues []QueueInfo
	err = json.Unmarshal(body, &queues)
	if err != nil {
		return nil, err
	}

	return queues, nil
}

func colorizeNumber(n int) string {
	if n == 0 {
		return fmt.Sprintf("[%d](fg:green)", n)
	} else if n < 100 {
		return fmt.Sprintf("[%d](fg:yellow)", n)
	} else {
		return fmt.Sprintf("[%d](fg:red)", n)
	}
}

func truncateString(s string, maxLength int) string {
	if s == "" {
		return strings.Repeat(" ", maxLength)
	}
	if len(s) <= maxLength {
		return s + strings.Repeat(" ", maxLength-len(s))
	}
	return s[:maxLength-3] + "..."
}

func safeGetFirstChar(s string) string {
	if len(s) > 0 {
		return string(s[0])
	}
	return "-"
}

func getStateIndicator(state string) string {
	if strings.ToLower(state) == "running" {
		return "✓"
	}
	return "✗"
}

func isErrorQueue(queueName string) bool {
	return strings.HasPrefix(strings.ToLower(queueName), "error") || strings.HasSuffix(strings.ToLower(queueName), "error")
}

// Beep sesi üreteci
type beepStreamer struct {
	freq float64
	t    float64
}

func (bs *beepStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		v := math.Sin(2 * math.Pi * bs.freq * bs.t)
		samples[i][0] = v
		samples[i][1] = v
		bs.t += 1.0 / 44100
	}
	return len(samples), true
}

func (bs *beepStreamer) Err() error {
	return nil
}

func playAlertSound() {
	if time.Since(lastAlertTime) < alertCooldown {
		return
	}
	sr := beep.SampleRate(44100)
	speaker.Init(sr, sr.N(time.Second/10))

	beeper := &beepStreamer{freq: 440} // 440 Hz (A4 nota)
	done := make(chan bool)
	speaker.Play(beep.Seq(beep.Take(sr.N(time.Second), beeper), beep.Callback(func() {
		done <- true
	})))
	<-done
	lastAlertTime = time.Now()
}

func main() {
	config, err := loadConfig("config.json")
	failOnError(err, "Failed to load configuration file")

	amqpURI := fmt.Sprintf("amqp://%s:%s@%s:%s/", config.RabbitMQ.Username, config.RabbitMQ.Password, config.RabbitMQ.Host, config.RabbitMQ.Port)
	conn, err := amqp.Dial(amqpURI)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	if err := termui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer termui.Close()

	table := widgets.NewTable()
	table.TextStyle = termui.NewStyle(termui.ColorWhite)
	table.TextAlignment = termui.AlignLeft
	table.BorderStyle = termui.NewStyle(termui.ColorCyan)
	table.RowSeparator = true
	table.FillRow = true

	updateTime := widgets.NewParagraph()
	updateTime.Text = "Last updated: N/A"
	updateTime.BorderStyle = termui.NewStyle(termui.ColorYellow)

	alertWidget := widgets.NewParagraph()
	alertWidget.Text = ""
	alertWidget.BorderStyle = termui.NewStyle(termui.ColorRed)

	updateTable := func() {
		queues, err := getQueues(config)
		if err != nil {
			log.Printf("Error listing queues: %s", err)
			return
		}

		width, height := termui.TerminalDimensions()

		queueNameWidth := width / 3
		otherColumnsWidth := (width - queueNameWidth - 4) / 7
		table.ColumnWidths = []int{queueNameWidth, 2, 2}
		for i := 0; i < 6; i++ {
			table.ColumnWidths = append(table.ColumnWidths, otherColumnsWidth)
		}

		rows := [][]string{
			{"Queue Name", "T", "S", "Ready", "Unacked", "Total", "In", "D/G", "Ack"},
		}

		errorQueuesFound := false
		for _, queue := range queues {
			if isErrorQueue(queue.Name) {
				errorQueuesFound = true
			}
			rows = append(rows, []string{
				truncateString(queue.VHost+"/"+queue.Name, queueNameWidth),
				safeGetFirstChar(queue.Type),
				getStateIndicator(queue.State),
				colorizeNumber(queue.MessagesReady),
				colorizeNumber(queue.MessagesUnack),
				colorizeNumber(queue.Messages),
				fmt.Sprintf("%d", queue.MessageStats.Publish),
				fmt.Sprintf("%d", queue.MessageStats.DeliverGet),
				fmt.Sprintf("%d", queue.MessageStats.Ack),
			})
		}

		table.Rows = rows

		for i := range table.Rows[0] {
			table.Rows[0][i] = fmt.Sprintf("[%s](fg:black,bg:yellow)", truncateString(table.Rows[0][i], table.ColumnWidths[i]))
		}

		updateTime.Text = fmt.Sprintf("Last updated: %s", time.Now().Format("2006-01-02 15:04:05"))

		if errorQueuesFound {
			alertWidget.Text = "ALERT: Error queue(s) detected!"
			alertWidget.TextStyle = termui.NewStyle(termui.ColorRed, termui.ColorClear, termui.ModifierBold)
			go playAlertSound()
		} else {
			alertWidget.Text = "No error queues detected."
			alertWidget.TextStyle = termui.NewStyle(termui.ColorGreen)
		}

		termui.Clear()
		table.SetRect(0, 0, width, height-6)
		updateTime.SetRect(0, height-6, width, height-3)
		alertWidget.SetRect(0, height-3, width, height)
		termui.Render(table, updateTime, alertWidget)
	}

	updateTable()

	uiEvents := termui.PollEvents()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			case "<Resize>":
				updateTable()
			}
		case <-ticker.C:
			updateTable()
		}
	}
}
