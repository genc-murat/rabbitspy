package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
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
		Publish        int     `json:"publish"`
		DeliverGet     int     `json:"deliver_get"`
		DeliverGetRate float64 `json:"deliver_get_details.rate"`
		Ack            int     `json:"ack"`
		AckRate        float64 `json:"ack_details.rate"`
	} `json:"message_stats"`
}

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

func printColoredNumber(n int) string {
	switch {
	case n == 0:
		return color.GreenString("%5d", n)
	case n < 100:
		return color.YellowString("%5d", n)
	default:
		return color.RedString("%5d", n)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s + strings.Repeat(" ", max-len(s))
	}
	return s[:max-3] + "..." + strings.Repeat(" ", max-len(s[:max-3]+"..."))
}

func main() {
	config, err := loadConfig("config.json")
	failOnError(err, "Failed to load configuration file")

	amqpURI := fmt.Sprintf("amqp://%s:%s@%s:%s/", config.RabbitMQ.Username, config.RabbitMQ.Password, config.RabbitMQ.Host, config.RabbitMQ.Port)
	conn, err := amqp.Dial(amqpURI)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	for {
		queues, err := getQueues(config)
		if err != nil {
			log.Printf("Error listing queues: %s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		fmt.Println("\nHostname                 Type     State   Ready Unacked Total  Incoming Deliver/Get     Ack")
		fmt.Println(strings.Repeat("-", 110))

		for _, queue := range queues {
			fmt.Printf("%-25s %-8s %-7s %s %s %s %8.2f %11.2f %11.2f\n",
				truncate(queue.VHost+"/"+queue.Name, 25),
				truncate(queue.Type, 8),
				truncate(queue.State, 7),
				printColoredNumber(queue.MessagesReady),
				printColoredNumber(queue.MessagesUnack),
				printColoredNumber(queue.Messages),
				float64(queue.MessageStats.Publish),
				queue.MessageStats.DeliverGetRate,
				queue.MessageStats.AckRate)
			fmt.Println(strings.Repeat("-", 110))
		}

		fmt.Println("\nRefreshing in 5 seconds...")
		time.Sleep(5 * time.Second)
	}
}
