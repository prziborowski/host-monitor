# Host Monitoring Application

A Go application that monitors host availability via ICMP ping and sends consolidated Slack alerts when hosts become unreachable.

## Features

- Monitors multiple hosts defined in a JSON configuration file
- Uses system ping command for reliable host checking
- Sends consolidated Slack alerts (updates same message instead of spamming)
- Special "All Systems Go" message when all hosts recover
- Verbose logging for debugging and monitoring
- Cross-platform support (Windows, macOS, Linux)

## Installation

### Prerequisites

- Go 1.21 or later
- Git

### Building

1. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/host-monitor.git
   cd host-monitor
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Build the application:
   ```bash
   go build -o host-monitor
   ```

## Configuration

Edit the `config/hosts.json` file to configure your monitoring:

```json
{
  "hosts": [
    {
      "name": "Web Server 1",
      "ip": "192.168.1.10"
    },
    {
      "name": "Database Server",
      "ip": "192.168.1.20"
    }
  ],
  "slack": {
    "api_key": "xoxb-your-slack-api-token",
    "channel": "#server-monitoring",
    "username": "Host Monitor",
    "icon_emoji": ":robot_face:"
  },
  "ping": {
    "timeout_seconds": 3,
    "max_failures": 5,
    "check_interval_seconds": 60
  }
}
```

### Configuration Options

- **hosts**: Array of hosts to monitor
  - `name`: Friendly name for the host
  - `ip`: IP address to ping
- **slack**: Slack notification configuration
  - `api_key`: Slack API token (create at https://api.slack.com/apps)
  - `channel`: Channel to post alerts to
  - `username`: Bot username
  - `icon_emoji`: Emoji for the bot
- **ping**: Ping configuration
  - `timeout_seconds`: Timeout for each ping attempt
  - `max_failures`: Number of consecutive failures before alerting
  - `check_interval_seconds`: How often to check each host

## Running the Application

### Basic Usage

```bash
./host-monitor
```

### Running in Background

```bash
nohup ./host-monitor > monitor.log 2>&1 &
```

### Running as a Service

Create a systemd service file (`/etc/systemd/system/host-monitor.service`):

```ini
[Unit]
Description=Host Monitoring Service
After=network.target

[Service]
User=yourusername
ExecStart=/path/to/host-monitor
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
```

Then enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable host-monitor
sudo systemctl start host-monitor
```

## Slack Setup

1. Create a Slack App at https://api.slack.com/apps
2. Add the `chat:write` and `chat:write.customize` permissions
3. Install the app to your workspace
4. Copy the Bot User OAuth Token (starts with `xoxb-`)
5. Add the token to your `config/hosts.json` file

## Logging

The application logs to stdout by default. To redirect logs:

```bash
./host-monitor > monitor.log 2>&1
```

## Troubleshooting

### Ping Not Working

If you see "operation not permitted" errors:
1. The application uses the system ping command, which should work without special permissions
2. Ensure the hosts are actually reachable from the machine running the monitor
3. Check firewall settings that might block ICMP packets

### Slack Notifications Not Sending

1. Verify the Slack API token is correct
2. Check that the bot has permission to post to the specified channel
3. Ensure the channel name is correct (including # prefix)

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please open an issue or pull request on GitHub.
