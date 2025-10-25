# WallDisplay

Raspberry Pi–based Sonos “now playing” monitor that can optionally mirror the current track to a 64×64 Adafruit RGB LED matrix (Bonnet + HUB75 panel).

The project is written in Go. When the `-display` flag is supplied, `matrixdisplay` drives the panel using the hzeller `rpi-rgb-led-matrix` driver, preconfigured for the Adafruit RGB Matrix Bonnet (`adafruit-hat-pwm` hardware mapping).

---

## 1. Prerequisites

- Raspberry Pi Zero W (or another Pi with a 40-pin header).
- Adafruit RGB Matrix Bonnet or HAT (newer PWM version) plus a 64×64 HUB75 matrix.
- Raspberry Pi OS Bookworm Lite (32-bit recommended for the Zero).
- Internet access while you install dependencies.
- Go 1.22+ (installable via `sudo apt install golang-go` or the official tarball).

Make sure the Pi, the matrix power supply, and your Sonos devices are on the same network.

---

## 2. Prepare the Raspberry Pi

1. Boot the Pi and connect via SSH or console.
2. Update default packages:
   ```sh
   sudo apt update && sudo apt full-upgrade -y
   ```
3. Install build prerequisites and Go if you have not already:
   ```sh
   sudo apt install -y git golang-go
   ```
4. Run Adafruit’s installer to configure the Bonnet and compile hzeller’s driver (this disables the Pi’s onboard audio and sets up the required services):
   ```sh
   cd ~
   curl -sS https://raw.githubusercontent.com/adafruit/Raspberry-Pi-Installer-Scripts/master/rgb-matrix.sh | sudo bash
   ```
   - Choose **Yes** when prompted to disable audio.
   - Select **Adafruit RGB Matrix Bonnet** and confirm the panel geometry (64×64, 1 chain).
   - Allow the script to compile the `rpi-rgb-led-matrix` library and reboot.
5. After reboot, verify the demos work (optional but recommended):
   ```sh
   sudo /home/pi/rpi-rgb-led-matrix/examples-api-use/demo --led-rows=64 --led-cols=64 --led-gpio-mapping=adafruit-hat-pwm
   ```
   You should see animations on the panel.

---

## 3. Fetch the project

```sh
git clone https://github.com/patojar/WallDisplay.git
cd WallDisplay
```

If you intend to copy the source from another machine, make sure the repository’s `matrixdisplay/controller_linux.go` stays intact (`HardwareMapping` must remain `adafruit-hat-pwm`).

---

## 4. Configuration (optional)

You can pin to one Sonos room by editing `config.json`:

```json
{
  "room": "Living Room"
}
```

Leave the file empty or delete it to display the status of every reachable room.

---

## 5. Build and run

You can run the app directly or build a binary. The display path needs elevated privileges to bit-bang the GPIO pins—either run with `sudo` or grant the executable the `cap_sys_nice`/`cap_sys_rawio` capabilities.

Run in one step:

```sh
sudo go run . -display
```

Or build first with the Makefile, then execute:

```sh
CGO_ENABLED=1 GOARCH=arm GOARM=6 make build
sudo ./bin/musicDisplay -display
```

Flags:

- `-display` enables the RGB matrix output. Without it, the app only prints Sonos status to the console.
- `-debug` adds verbose logging for discovery and event handling.

When the program starts it:

1. Discovers Sonos zones on your LAN.
2. (If `config.json` specifies a room) subscribes to real-time events for that zone.
3. Displays the current track on stdout, and mirrors artwork/text on the matrix when `-display` is set.

Press `Ctrl+C` to exit cleanly.

---

## 6. Troubleshooting tips

- If the panel stays dark, re-run Adafruit’s installer and confirm you are using the PWM bonnet mapping. The project hardcodes `adafruit-hat-pwm`, so the underlying driver must match the same wiring.
- Flicker or super-dim output usually means the mapping is wrong or the matrix PSU is undersized—64×64 panels need a dedicated 5 V supply that can source 4 A or more.
- Network discovery relies on SSDP; make sure mDNS/SSDP traffic is not blocked between the Pi and your Sonos devices.
- Running without `sudo` triggers “GPIO permission denied” errors. Either use `sudo` or set the necessary capabilities on the binary (`sudo setcap 'cap_sys_nice,cap_sys_rawio=+ep' ./bin/walldisplay`).

---

## 7. Development on non-Linux hosts

The matrix driver only compiles on Linux (`//go:build linux`). On macOS or Windows you can still develop other parts of the project—the stub in `matrixdisplay/controller_stub.go` will disable display support, so `go run` works without the `-display` flag.

---

Happy hacking!
