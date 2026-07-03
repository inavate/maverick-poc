# Maverick POC

Standalone Go server for a hosted card payment proof of concept.

The app:

- Serves the UI from `index.html`, `styles.css`, and `app.js`
- Exposes backend endpoints under `/api/v1/customer/maverick/*`
- Calls Inavate OAuth and Maverick APIs using values in `.env`

## Prerequisites

- Go 1.20+
- Network access to your Inavate API base URL
- Valid Inavate credentials (client id/secret, merchant/location ids)

## Install Go

### Windows

1. Download the Go installer from https://go.dev/dl/
2. Run the `.msi` installer and keep default options.
3. Open a new terminal and verify:

```powershell
go version
```

If `go` is not recognized, restart your terminal (or machine) and check that Go is on PATH.

### macOS

Install with Homebrew:

```bash
brew install go
go version
```

Or install from https://go.dev/dl/ using the macOS package.

### Linux

Option A (official tarball, recommended):

```bash
# Example for Linux amd64. Use latest version from https://go.dev/dl/
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
go version
```

Option B (package manager):

```bash
# Debian/Ubuntu
sudo apt update
sudo apt install -y golang-go
go version
```

## Project Setup

1. Clone the repository and move into this folder.
2. Create a `.env` file in the project root.
3. Add the required variables:

```dotenv
PORT=8002
INAVATE_API_URL=https://your-inavate-api-url
INAVATE_CLIENT_ID=your-client-id
INAVATE_CLIENT_SECRET=your-client-secret
INAVATE_MERCHANT_ID=your-merchant-id
INAVATE_LOCATION_ID=your-location-id
```

Notes:

- `PORT` defaults to `8002` if omitted.
- Do not commit real secrets.

## Run Locally

From the project root:

```bash
go mod tidy
go run .
```

You should see a startup log similar to:

```text
Starting Maverick POC standalone server on http://localhost:8002
```

Then open:

- http://localhost:8002

## HTTPS with ngrok (if required)

Use this when a third-party integration requires an HTTPS origin/domain instead of `http://localhost`.

1. Install ngrok from https://ngrok.com/download
2. Authenticate ngrok once:

```bash
ngrok config add-authtoken <your-ngrok-token>
```

3. Start this app locally:

```bash
go run .
```

4. In another terminal, expose port `8002`:

```bash
ngrok http 8002
```

5. Use the generated HTTPS URL (for example, `https://abc123.ngrok-free.app`) to open the app.

Notes:

- Keep the ngrok process running while testing.
- If your provider allows domain/origin allowlists, add your current ngrok HTTPS domain.
- Free ngrok URLs can change between sessions; update any allowlists/webhook settings when that happens.

## Available Endpoints

- `POST /api/v1/customer/maverick/init-payment`
- `POST /api/v1/customer/maverick/save-card`
- `POST /api/v1/customer/maverick/charge`

## Troubleshooting

- `go: command not found` or `go is not recognized`:
  - Reopen terminal and run `go version`.
  - Confirm Go is installed and PATH is set.
- `Server misconfigured: missing environment variables`:
  - Verify `.env` exists in project root and contains all required variables.
- OAuth/API failures:
  - Confirm credentials and base URL are valid.
  - Confirm outbound network access to Inavate endpoints.
