package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

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
	Name          string   `json:"name"`
	VHost         string   `json:"vhost"`
	Type          string   `json:"type"`
	State         string   `json:"state"`
	Features      []string `json:"features"`
	Messages      int      `json:"messages"`
	MessagesReady int      `json:"messages_ready"`
	MessagesUnack int      `json:"messages_unacknowledged"`
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	for {
		queues, err := getQueues(config)
		if err != nil {
			log.Printf("Error listing queues: %s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		fmt.Fprintln(w, "Hostname\tType\tFeatures\tState\tReady\tUnacked\tTotal\tIncoming\tDeliver/Get\tAck")
		fmt.Fprintln(w, "--------\t----\t--------\t-----\t-----\t-------\t-----\t--------\t------------\t---")

		for _, queue := range queues {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%.2f\t%.2f\t%.2f\n",
				queue.VHost+"/"+queue.Name,
				queue.Type,
				strings.Join(queue.Features, ","),
				queue.State,
				queue.MessagesReady,
				queue.MessagesUnack,
				queue.Messages,
				float64(queue.MessageStats.Publish),
				queue.MessageStats.DeliverGetRate,
				queue.MessageStats.AckRate)
		}

		w.Flush()
		fmt.Println("\nRefreshing in 5 seconds...")
		time.Sleep(5 * time.Second)
	}
}
