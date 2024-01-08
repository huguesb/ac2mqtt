# ac2mqtt

Inspired by zigbee2mqtt, ruuvi-go-gateway and ruuvi-bridge, ac2mqtt is intended to expose AC Infinity Controllers as HomeAssistant-friendly MQTT entities.

### Features

- Supports most BLE controller models as of August 2023
- Publishes sensor entities for temperature, humidity, and vapour-pressure deficit
- Publishes fan entities that support on/off switch and speed adjustment
- Supports multi-port controllers

### Runtime Requirements

- Linux-based OS
- Bluetooth adapter supporting Bluetooth Low Energy
- 10MB of disk space
- ~20MB of RAM

### Build Requirements

- [Go](https://golang.org) 1.18+

### Configuration

Check [config.sample.yml](./config.sample.yml) for a sample config. By default the gateway assumes to find a file called `config.yml` in the current working directory, but that can be overridden with `-config /path/to/config.yml` command line flag.

### Installation

```
make
sudo make install
```

Note that running the standalone binaries without root requires some extra capabilities be set to the binary to grant it permissions to scan for ble, this can be done with:

```sh
sudo setcap 'cap_net_raw,cap_net_admin+eip' ./ac2mqtt
```
