# Rabbit Spy

<div align="center">
    <img src="/rabbitspy.png" alt="RabbitSpy Icon">
</div>


**Rabbit Spy** is a terminal-based monitoring tool for RabbitMQ queues. It provides a real-time overview of the status of all queues within a RabbitMQ server, including metrics like the number of messages, message states, and message stats.

## Features

- Real-time monitoring of RabbitMQ queues.
- Displays queue statistics such as message count, ready messages, unacknowledged messages, and message state.
- Color-coded output for better visibility of important metrics.
- Automatic table resizing based on terminal window size.
- Updates every 5 seconds to provide near real-time data.

## Installation

1. **Clone the repository:**
   ```bash
   git clone https://github.com/genc-murat/rabbit-spy.git
   cd rabbit-spy
   ```

2. **Install dependencies:**
   Make sure you have Go installed (version 1.16 or later). You can download and install Go from [the official website](https://golang.org/dl/).

3. **Build the application:**
   ```bash
   go build -o rabbit-spy
   ```

## Configuration

Rabbit Spy requires a configuration file (`config.json`) to connect to your RabbitMQ instance. The configuration file should be in the following format:

```json
{
  "rabbitmq": {
    "username": "guest",
    "password": "guest",
    "host": "localhost",
    "port": "5672",
    "management_port": "15672"
  }
}
```

Place this `config.json` file in the same directory as the Rabbit Spy executable.

## Usage

1. **Run the application:**
   ```bash
   ./rabbit-spy
   ```

2. **Key Commands:**
   - `q` or `Ctrl+C` to quit the application.
   - Resize the terminal window to automatically adjust the table.

## Dependencies

Rabbit Spy uses the following Go libraries:

- [gizak/termui](https://github.com/gizak/termui) - For terminal-based UI components.
- [rabbitmq/amqp091-go](https://github.com/rabbitmq/amqp091-go) - For AMQP protocol support.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contribution

Contributions are welcome! Please fork the repository and submit a pull request for any enhancements, bug fixes, or new features.

## Support

If you encounter any issues or have questions, please open an issue on the [GitHub repository](https://github.com/genc-murat/rabbit-spy/issues).
