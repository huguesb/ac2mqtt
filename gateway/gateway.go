package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
	"github.com/go-ble/ble/linux/hci/cmd"
	"github.com/huguesb/ac2mqtt/config"
	log "github.com/sirupsen/logrus"
)

type mqttPub struct {
	topic   string
	payload []byte
}

type mqttState struct {
	c    mqtt.Client
	done atomic.Bool
	err  chan error
	snd  chan mqttPub
	rcv  chan mqtt.Message
}

type Gateway struct {
	config config.Config

	ble *linux.Device

	ticker *time.Ticker

	mqtt atomic.Pointer[mqttState]

	clients map[string]*ACDevice
}

var GenericAccessUUID = ble.MustParse("1800")
var DeviceInformationUUID = ble.MustParse("180a")
var ACInfinitySvcUUID = ble.MustParse("70D51000-2C7F-4E75-AE8A-D758951CE4E0")

// var ACInfinityNotifyUUID = ble.MustParse("70D51002-2C7F-4E75-AE8A-D758951CE4E0")
var ACInfinityWriteUUID = ble.MustParse("70D51001-2C7F-4E75-AE8A-D758951CE4E0")

var charMap = map[string]map[string]string{
	GenericAccessUUID.String(): {
		"2a00": "Device Name",
	},
	DeviceInformationUUID.String(): {
		"2a24": "Model Number",
		"2a25": "Serial Number",
		"2a26": "Firmware Version",
		"2a27": "Hardware Version",
		"2a28": "Software Version",
		"2a29": "Manufacturer",
	},
}

func NewGateway(config config.Config) *Gateway {
	g := new(Gateway)
	g.config = config
	g.clients = make(map[string]*ACDevice)
	return g
}

func mqttOptions(conf *config.MQTT) *mqtt.ClientOptions {
	address := conf.BrokerAddress
	if address == "" {
		address = "localhost"
	}
	port := conf.BrokerPort
	if port == 0 {
		port = 1883
	}
	server := conf.BrokerUrl
	if server == "" {
		server = fmt.Sprintf("tcp://%s:%d", address, port)
	}
	clientID := conf.ClientID
	if clientID == "" {
		clientID = "ac-2-mqtt"
	}
	opts := mqtt.NewClientOptions()
	opts.SetCleanSession(false)
	opts.AddBroker(server)
	opts.SetClientID(clientID)
	opts.SetUsername(conf.Username)
	opts.SetPassword(conf.Password)
	opts.SetKeepAlive(10 * time.Second)
	opts.SetAutoReconnect(false)
	opts.SetMaxReconnectInterval(10 * time.Second)
	if conf.LWTTopic == nil || *conf.LWTTopic != "" {
		topic := conf.TopicPrefix + "/status"
		payload := conf.LWTOfflinePayload
		if conf.LWTTopic != nil {
			topic = *conf.LWTTopic
		}
		if payload == "" {
			payload = "{\"state\":\"offline\"}"
		}
		opts.SetWill(topic, payload, 0, true)
	}
	return opts
}

func (g *Gateway) Start() context.Context {
	ctx, _ := context.WithCancel(context.Background())
	if g.config.MQTT != nil && (g.config.MQTT.Enabled == nil || *g.config.MQTT.Enabled) {
		go g.mqttRetryLoop(ctx)
	} else {
		log.Warn("MQTT is not configured, check the config")
	}

	if !g.withHass() {
		log.Info("HomeAssistant discovery disabled")
	}

	go g.bleRetryLoop(ctx)

	if g.config.MQTT.RefreshPeriod != nil {
		g.ticker = time.NewTicker(*g.config.MQTT.RefreshPeriod)
		go g.periodicStateDumper(ctx)
	}

	return ctx
}

func (g *Gateway) bleRetryLoop(ctx context.Context) {
	for {
		err := g.bleScan(ctx)
		if err == nil {
			break
		}
		log.WithError(err).WithFields(log.Fields{
			"hci_index": g.config.HciIndex,
		}).Error("BLE scan failed. Retrying...")
		// TODO: capped exponential backoff?
	}
}

func (g *Gateway) bleScan(ctx context.Context) error {
	device, err := linux.NewDeviceWithName("default",
		ble.OptDeviceID(g.config.HciIndex),
		ble.OptScanParams(cmd.LESetScanParameters{
			LEScanType:     0, // passive scan
			LEScanInterval: 1000,
		}))
	if err != nil {
		return err
	}
	ble.SetDefaultDevice(device)
	g.ble = device
	return ble.Scan(ctx, true, g.HandleAdvertisement, nil)
}

func (g *Gateway) mqttRetryLoop(ctx context.Context) {
	for {
		s := g.mqttConnect()
		select {
		case err := <-s.err:
			log.WithError(err).Error("mqtt error")
			s.done.Store(true)
		case <-ctx.Done():
			return
		}
	}
}

func (g *Gateway) mqttConnect() *mqttState {
	s := new(mqttState)
	s.err = make(chan error, 1)
	s.snd = make(chan mqttPub, 100)
	s.rcv = make(chan mqtt.Message, 100)

	client := mqtt.NewClient(mqttOptions(g.config.MQTT))
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		s.err <- token.Error()
		return s
	}
	if g.config.MQTT.LWTTopic == nil || *g.config.MQTT.LWTTopic != "" {
		topic := g.config.MQTT.TopicPrefix + "/status"
		payload := g.config.MQTT.LWTOnlinePayload
		if g.config.MQTT.LWTTopic != nil {
			topic = *g.config.MQTT.LWTTopic
		}
		if payload == "" {
			payload = "{\"state\":\"online\"}"
		}
		if token := client.Publish(topic, 0, true, payload); token.Wait() && token.Error() != nil {
			s.err <- token.Error()
			return s
		}
	}
	s.c = client
	g.mqtt.Store(s)

	// wildcard subscribe to command topics
	token := client.Subscribe(g.config.MQTT.TopicPrefix+"/+/set/+", 0, g.onCommandMessage)
	if token.Wait() && token.Error() != nil {
		log.WithError(token.Error()).Warn("Failed to subscribe to wildcard command topic")
		s.err <- token.Error()
		return s
	}

	if g.withHass() {
		// subscribe to homeassistant status channel to automatically re-publish state
		ha_lwt_topic := "homeassistant/status"
		if g.config.HomeAssistant.LWTTopic != nil {
			ha_lwt_topic = *g.config.HomeAssistant.LWTTopic
		}
		token := client.Subscribe(ha_lwt_topic, 0, g.onHassStatus)
		if token.Wait() && token.Error() != nil {
			log.WithError(token.Error()).Warn("Failed to subscribe to ", ha_lwt_topic)
			s.err <- token.Error()
			return s
		}

		go g.hassDiscoveryRefresh()
	}

	go g.mqttPublisher(s)
	go g.mqttReceiver(s)

	return s
}

func (g *Gateway) periodicStateDumper(ctx context.Context) {
	for {
		select {
		case <-g.ticker.C:
			for _, c := range g.clients {
				c.sendStateDump()
			}
		case <-ctx.Done():
			return
		}
	}
}

func (g *Gateway) mqttPublisher(s *mqttState) {
	// serialize emission of mqtt messages via a single goroutine
	for {
		p := <-s.snd
		// this connection is done, all buffered messages will be abandonned
		if s.done.Load() {
			break
		}

		log.Debug("mqtt pub: ", p.topic, " ", string(p.payload))
		token := s.c.Publish(p.topic, 0, false, p.payload)
		if token.Wait() && token.Error() != nil {
			s.err <- token.Error()
			break
		}
	}
}

func (g *Gateway) mqttReceiver(s *mqttState) {
	// serialize handling of incoming mqtt messages in a single goroutine
	for {
		msg := <-s.rcv
		// this connection is done, all buffered messages will be abandonned
		// TODO: best-effort handling of commands? or at least log dropped commands?
		if s.done.Load() {
			break
		}
		t := msg.Topic()
		log.Debug("mqtt cmd: ", t, msg.Payload())
		if !strings.HasPrefix(t, g.config.MQTT.TopicPrefix) {
			log.Warn("Unexpected message: topic=", t, " payload=", msg.Payload())
			return
		}
		path := strings.SplitN(t[len(g.config.MQTT.TopicPrefix)+1:], "/", 2)
		id_and_maybe_port, cmd := path[0], path[1]
		id_parts := strings.SplitN(id_and_maybe_port, "-", 2)
		id := id_parts[0]
		c, present := g.clients[id]
		if !present {
			log.Debug("Discard message for unknown device: ", id)
			return
		}

		c.onCommandMessage(id_and_maybe_port, cmd, msg.Payload())
	}
}

func (g *Gateway) publish(topic string, d interface{}) {
	s := g.mqtt.Load()
	if s == nil {
		return
	}
	var payload []byte

	switch v := d.(type) {
	case string:
		payload = []byte(v)
	case []byte:
		payload = v
	default:
		if enc, err := json.Marshal(d); err != nil {
			panic(err)
		} else {
			payload = enc
		}
	}

	s.snd <- mqttPub{topic: topic, payload: payload}
}

func (g *Gateway) withHass() bool {
	return g.config.HomeAssistant != nil && (g.config.HomeAssistant.Enabled != nil &&
		*g.config.HomeAssistant.Enabled)
}

func (g *Gateway) onHassStatus(client mqtt.Client, msg mqtt.Message) {
	log.Info("hass status: ", msg.Payload())

	if string(msg.Payload()) == "online" {
		// home assistant instance came online: send a state dump
		go g.hassDiscoveryRefresh()
	}
}

func (g *Gateway) hassDiscoveryRefresh() {
	for _, c := range g.clients {
		c.sendHassDiscoveryMessage()
	}
}

func (g *Gateway) onCommandMessage(client mqtt.Client, msg mqtt.Message) {
	s := g.mqtt.Load()
	if s == nil {
		return
	}
	s.rcv <- msg
}

func (g *Gateway) HandleAdvertisement(adv ble.Advertisement) {
	data := adv.ManufacturerData()
	// official AC infinity app filters by a 16bit id attached to the BLE
	// manufacturer data field. Our BLE library doesn't extract it for us
	// and instead leaves it at the start of the byte array
	// Official Android App only recignizes two valid length for advertisement
	// packets, so we do the same filtering for robustness
	// NB: could also filter by manufacturer MAC prefix: "34:85:18"
	if (len(data) == 19 || len(data) == 29) &&
		data[0] == 0x02 && data[1] == 0x09 {
		log.WithFields(log.Fields{
			"mac":  strings.ToUpper(adv.Addr().String()),
			"rssi": adv.RSSI(),
			"conn": adv.Connectable(),
			"data": fmt.Sprintf("%X", data),
		}).Debug("New AC Infinity device")
		// NB: filter out the manufacturer 16bit id from scan data
		go g.onNewDevice(adv.Addr(), data[2:])
	}
}

func (g *Gateway) onNewDevice(addr ble.Addr, scanData []byte) {
	mac := addr.String()
	client, err := ble.Dial(context.Background(), addr)
	if err != nil {
		log.WithError(err).Warn("Can't connect to device ", mac)
		return
	}

	log.Trace("connected ", mac)

	// same as AC infinity Android app...
	client.ExchangeMTU(247)
	log.Trace("mtu exchanged ", mac)

	// NB: this is responsible for creating BLE subscribe
	c := NewACDevice(g, client, scanData)
	if c == nil {
		return
	}
	g.clients[c.id] = c

	if g.withHass() {
		c.sendHassDiscoveryMessage()
	}

	// NB: block this goroutine until the BLE client disconnects
	<-c.c.Disconnected()
	log.Debug("disconnected ", mac)
	delete(g.clients, c.id)

	if g.withHass() {
		// TODO: send disappearance message?
	}
}
