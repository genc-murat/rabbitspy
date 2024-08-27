package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rabbitmq/amqp091-go"
)

// Config structure for RabbitMQ settings
type Config struct {
	RabbitMQ struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		Host           string `json:"host"`
		Port           string `json:"port"`
		ManagementPort string `json:"management_port"`
	} `json:"rabbitmq"`
}

// Item represents a queue for selection
type item struct {
	title string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return "" }
func (i item) FilterValue() string { return i.title }

// Model structure for Bubble Tea
type model struct {
	list       list.Model
	queues     []string
	selected   map[int]struct{}
	quitting   bool
	messages   map[string][]string
	conn       *amqp091.Connection
	channel    *amqp091.Channel
	config     *Config
	queueList  []string
	monitoring bool
}

// Function to load configuration from JSON file
func loadConfig(filePath string) (*Config, error) {
	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// Function to connect to RabbitMQ using config
func connectRabbitMQ(config *Config) (*amqp091.Connection, *amqp091.Channel, error) {
	rabbitmqURL := fmt.Sprintf(
		"amqp://%s:%s@%s:%s/",
		config.RabbitMQ.Username,
		config.RabbitMQ.Password,
		config.RabbitMQ.Host,
		config.RabbitMQ.Port,
	)

	conn, err := amqp091.Dial(rabbitmqURL)
	if err != nil {
		return nil, nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close() // Ensure connection is closed if channel creation fails
		return nil, nil, err
	}

	return conn, ch, nil
}

// Function to dynamically fetch queue names from RabbitMQ Management API
func fetchQueueNames(config *Config) ([]string, error) {
	url := fmt.Sprintf("http://%s:%s/api/queues", config.RabbitMQ.Host, config.RabbitMQ.ManagementPort)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(config.RabbitMQ.Username, config.RabbitMQ.Password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var queues []struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&queues); err != nil {
		return nil, err
	}

	queueNames := make([]string, len(queues))
	for i, queue := range queues {
		queueNames[i] = queue.Name
	}

	return queueNames, nil
}

// Initialize the Bubble Tea model
func initialModel(config *Config, queues []string) model {
	items := make([]list.Item, len(queues))
	for i, queue := range queues {
		items[i] = item{title: queue}
	}

	const defaultWidth = 20
	const defaultHeight = 14 // Set an appropriate height for the list

	l := list.New(items, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	l.Title = "Select queues to monitor (Press Space to select/unselect, Enter to confirm selection):"

	return model{
		list:       l,
		messages:   make(map[string][]string),
		config:     config,
		queueList:  queues,
		selected:   make(map[int]struct{}),
		monitoring: false,
	}
}

// Bubble Tea update function
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if !m.monitoring {
				// Confirm selection and start monitoring
				for i := range m.selected {
					item, ok := m.list.Items()[i].(item)
					if ok {
						m.queues = append(m.queues, item.FilterValue())
					}
				}
				log.Printf("Selected queues: %s", strings.Join(m.queues, ", "))
				if err := m.startMonitoring(); err != nil {
					log.Fatalf("Failed to start monitoring: %v", err)
				}
				m.monitoring = true
			}
		case " ":
			// Toggle selection
			index := m.list.Index()
			if _, ok := m.selected[index]; ok {
				delete(m.selected, index)
			} else {
				m.selected[index] = struct{}{}
			}
		}

	case tickMsg:
		if m.monitoring {
			return m, tea.Tick(time.Second, tick)
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// Custom message type for ticks
type tickMsg time.Time

// Helper function for creating tick messages
func tick(t time.Time) tea.Msg {
	return tickMsg(t)
}

// Bubble Tea view function
func (m model) View() string {
	if m.quitting {
		return "Exiting...\n"
	}

	s := "RabbitMQ Messages:\n\n"

	for queue, msgs := range m.messages {
		s += fmt.Sprintf("Queue: %s\n", queue)
		for _, msg := range msgs {
			s += msg + "\n"
		}
		s += "\n"
	}

	if m.monitoring {
		s += "\nPress 'q' to quit.\n"
	} else {
		s = m.list.View()
	}

	return s
}

// Bubble Tea init function
func (m model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(time.Second, tick))
}

// Start monitoring selected queues
func (m *model) startMonitoring() error {
	conn, ch, err := connectRabbitMQ(m.config)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	m.conn = conn
	m.channel = ch

	for _, queueName := range m.queues {
		_, err := ch.QueueDeclarePassive(
			queueName, // Queue name
			true,      // Durable
			false,     // Exclusive
			false,     // Auto delete
			false,     // No-wait
			nil,       // Arguments
		)
		if err != nil {
			log.Printf("Queue %s does not exist or error occurred: %v", queueName, err)
			if ch.IsClosed() {
				log.Println("Channel is closed, attempting to reconnect...")
				return fmt.Errorf("channel is closed")
			}
			continue
		}

		go m.consumeQueue(queueName)
	}

	return nil
}

// Consume messages from a specified queue
func (m *model) consumeQueue(queueName string) {
	for {
		msgs, err := m.channel.Consume(
			queueName, // Queue name
			"",        // Consumer tag
			true,      // Auto ACK
			false,     // Exclusive
			false,     // NoLocal
			false,     // NoWait
			nil,       // Arguments
		)
		if err != nil {
			log.Printf("Failed to consume messages from %s: %v", queueName, err)
			if m.channel.IsClosed() {
				log.Println("Channel is closed, stopping consumer goroutine...")
				return
			}
			time.Sleep(5 * time.Second) // Wait before retrying
			continue
		}

		for d := range msgs {
			msg := fmt.Sprintf("[%s] Received message: %s", queueName, d.Body)
			m.messages[queueName] = append(m.messages[queueName], msg)
		}

		time.Sleep(5 * time.Second) // Wait before retrying
	}
}

func main() {
	// Load configuration from JSON file
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Fetch queue names dynamically
	queueNames, err := fetchQueueNames(config)
	if err != nil {
		log.Fatalf("Failed to fetch queue names: %v", err)
	}

	m := initialModel(config, queueNames)

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Failed to run program: %v", err)
		os.Exit(1)
	}
}
