package gateway

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/huguesb/ble"
	log "github.com/sirupsen/logrus"
)

type PortType int

const (
	UnknownPort PortType = 0
	Fan                  = 2
	Light                = 3
	Plug                 = 4
	AC                   = 5
	Humidifer            = 6
	Dehumidifer          = 7
	Heater               = 8
)

type ACDevicePortInfo struct {
	isPlug            bool
	fanType           uint16
	fanState          byte // 2 bits
	overcurrentStatus byte // 2 bits
	isAdv             byte // 4 bits
	fan               byte // 4 bits
	workType          byte // 4 bits

	// V6 fields
	loadState byte // 1 bit
	errState  byte // 1 bit
	portType  uint16
}

func (pi *ACDevicePortInfo) isFan() bool {
	return pi.isPlug && (pi.portType == Fan || portTypeByResistance(int(pi.fanType)) == Fan)
}

type ACDeviceState struct {
	isDegree          bool
	tmpState          byte   // 2 bits
	humState          byte   // 2 bits
	vpdState          byte   // 2 bits
	port              byte   // 4 bits
	tmp               uint16 // (in 1/100th of C)
	hum               uint16 // (in 1/100th of %)
	vpd               uint16 // (in 1/100th of kPa)
	fanType           uint16
	fanState          byte // 2 bits
	overcurrentStatus byte // 2 bits
	isAdv             byte // 4 bits
	fan               byte // 4 bits
	workType          byte // 4 bits

	// v6 fields
	loadState byte // 1 bit (use bool?)
	errState  byte // 1 bit (use bool?)

	portInfo []ACDevicePortInfo
}

type ACDevice struct {
	gw *Gateway
	c  ble.Client
	w  *ble.Characteristic

	id string // unique ID (BLE MAC)

	Type       int8
	Version    int8
	PortCount  int8
	properties map[string]string // static properties queried on initial connection

	seq         int
	latestState atomic.Pointer[ACDeviceState]
}

func (s *ACDeviceState) String() string {
	ps := make([]string, len(s.portInfo))
	for i, p := range s.portInfo {
		ps[i] = fmt.Sprintf(
			"Port(isPlug=%v,fanType=%x,fanState=%x,"+
				"overcurrentStatus=%x,isAdv=%x,fan=%x,workType=%x,portType=%d"+")",
			p.isPlug, p.fanType, p.fanState,
			p.overcurrentStatus, p.isAdv, p.fan, p.workType, p.portType,
		)
	}
	return fmt.Sprintf(
		"State(isDegree=%v,tmpState=%x,humState=%x, vpdState=%x,port=%d,"+
			"tmp=%d,hum=%d,vpd=%d,"+
			"fanType=%x,fanState=%x,overcurrentStatus=%x,"+
			"isAdv=%x,fan=%x,workType=%x"+")",
		s.isDegree, s.tmpState, s.humState, s.vpdState, s.port,
		s.tmp, s.hum, s.vpd,
		s.fanType, s.fanState, s.overcurrentStatus,
		s.isAdv, s.fan, s.workType,
	) + "{\n\t" + strings.Join(ps, "\n\t") + "\n}"
}

func (s *ACDeviceState) FromNotif(req []byte) {
	if len(req) < 18 {
		return
	}

	b := req[6]
	s.isDegree = !getBit(b, 0)
	s.tmpState = getBits(b, 1, 2)
	s.humState = getBits(b, 3, 2)
	s.vpdState = getBits(b, 5, 2)
	s.port = getBits(req[7], 4, 4)
	s.tmp = getShort(req, 8)
	s.hum = getShort(req, 10)
	s.vpd = getShort(req, 12)
	s.fanType = getShort(req, 14)
	s.fanState = getBits(req[16], 0, 2)
	s.overcurrentStatus = getBits(req[16], 2, 2)
	s.isAdv = getBits(req[16], 4, 4)
	s.fan = getBits(req[17], 0, 4)
	s.workType = getBits(req[17], 4, 4)

	s.portInfo = make([]ACDevicePortInfo, (len(req)-18)/4)

	for i := 0; i < len(s.portInfo); i += 1 {
		idx := 18 + 4*i
		s.portInfo[i] = ACDevicePortInfo{
			isPlug:            req[idx] != 0xff,
			fanType:           getShort(req, idx),
			fanState:          getBits(req[idx+2], 0, 2),
			overcurrentStatus: getBits(req[idx+2], 2, 2),
			isAdv:             getBits(req[idx+2], 4, 4),
			fan:               getBits(req[idx+3], 0, 4),
			workType:          getBits(req[idx+3], 4, 4),
		}
	}
}

func (s *ACDeviceState) FromNotifV6(req []byte) {
	if len(req) < 22 {
		return
	}

	b := req[6]
	s.isDegree = !getBit(b, 0)
	s.tmpState = getBits(b, 1, 2)
	s.humState = getBits(b, 3, 2)
	s.vpdState = getBits(b, 5, 2)
	s.port = getBits(req[7], 4, 4)
	s.tmp = getShort(req, 8)
	s.hum = getShort(req, 10)
	s.vpd = getShort(req, 12)
	s.fanState = getBits(req[16], 0, 2)
	s.fan = getBits(req[16], 2, 4)
	s.loadState = getBits(req[16], 6, 1)
	s.errState = getBits(req[16], 7, 1)
	s.overcurrentStatus = getBits(req[17], 0, 1)
	s.isAdv = getBits(req[17], 1, 3)
	s.workType = getBits(req[17], 4, 4)
	s.fanType = convertPortTypeToLocal(req[21])

	pd := req[22:]
	s.portInfo = make([]ACDevicePortInfo, len(pd)/8)

	for i := 0; i < len(s.portInfo); i += 1 {
		idx := i << 3
		s.portInfo[i] = ACDevicePortInfo{
			isPlug:            pd[idx] != 0xff,
			fanType:           getShort(pd, idx),
			fanState:          getBits(pd[idx+2], 0, 2),
			fan:               getBits(pd[idx+2], 2, 4),
			loadState:         getBits(pd[idx+2], 6, 1),
			errState:          getBits(pd[idx+2], 7, 1),
			overcurrentStatus: getBits(pd[idx+3], 0, 1),
			isAdv:             getBits(pd[idx+3], 1, 3),
			workType:          getBits(pd[idx+3], 4, 4),
			portType:          convertPortTypeToLocal(pd[idx+7]),
		}
	}
}

func NewACDevice(g *Gateway, client ble.Client, scanData []byte) *ACDevice {
	c := new(ACDevice)
	c.id = strings.ReplaceAll(client.Addr().String(), ":", "")
	c.c = client
	c.gw = g
	c.properties = make(map[string]string)

	c.Type = int8(scanData[12])
	c.Version = int8(scanData[11])
	if len(scanData) > 23 {
		c.PortCount = int8(scanData[23])
	}

	p, err := client.DiscoverProfile(false)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"mac": client.Addr().String(),
		}).Warn("Can't discover device profile")
		client.CancelConnection()
		return nil
	}

	for _, svc := range p.Services {
		cm, present := charMap[svc.UUID.String()]
		if present {
			for _, chr := range svc.Characteristics {
				name, present := cm[chr.UUID.String()]
				if present && chr.Property&ble.CharRead != 0 {
					if v, err := client.ReadCharacteristic(chr); err == nil {
						c.properties[name] = string(v)
						log.Trace(fmt.Sprintf("%s: %s", name, v))
					}
				}
			}
		} else if svc.UUID.Equal(ACInfinitySvcUUID) {
			for _, chr := range svc.Characteristics {
				if chr.Property&ble.CharNotify != 0 {
					if err := client.Subscribe(chr, false, c.HandleNotification); err == nil {
						log.Debug("subscribed ", chr.UUID.String())
					} else {
						log.WithError(err).Trace("failed to subscribe")
					}
				}
				if chr.UUID.Equal(ACInfinityWriteUUID) && chr.Property&ble.CharWrite != 0 {
					// record the write characteristic because we need the underlying
					// handle to send commands later on
					c.w = chr
				}
			}
		}
	}

	log.Info(fmt.Sprintf("Connected to %s (type:%d, version:%d, ports:%d)",
		c.properties["Device Name"], c.Type, c.Version, c.PortCount))

	typeName := string(scanData[6:11])
	// the protocol deals in Celsius values, this flag merely suggests display format
	isDegree := !getBit(scanData[13], 1)
	fanState := getBits(scanData[13], 2, 2)
	tmpState := getBits(scanData[13], 4, 2)
	humState := getBits(scanData[13], 6, 2)
	tmp := getShort(scanData, 14)
	hum := getShort(scanData, 16)
	fan := scanData[18]
	log.Info(typeName, " ", isDegree, fanState, tmpState, humState, tmp, hum, fan)
	if c.Version >= 3 && isDeviceMultiPort(int(c.Type)) {
		choosePort := scanData[19]
		vpdState := getBits(scanData[20], 0, 2)
		vpd := getShort(scanData, 21)
		log.Info(choosePort, vpdState, vpd)
	}
	// TODO: set initial state?

	return c
}

func (c *ACDevice) HandleNotification(req []byte) {
	log.Debug(fmt.Sprintf("notified: %x", req))

	if len(req) < 5 || !bytes.Equal(req[0:4], []byte{0x1e, 0xff, 0x02, 0x09}) {
		return
	}
	switch c.Type {
	case 4, 5, 14, 15, 24, 25, 34, 35:
		// these are not controllers, we're not going to support them...
	default:
		if req[5] == 0 || len(req) < int(req[5])+6 {
			return
		}

		var s ACDeviceState
		if c.Version >= 5 {
			s.FromNotifV6(req)
		} else {
			s.FromNotif(req)
		}

		log.Debug(s.String())
		prev := c.latestState.Swap(&s)
		c.publishStateDiff(prev, &s)
	}
}

func (c *ACDevice) sendStateDump() {
	s := c.latestState.Load()
	if s != nil {
		c.publishStateDiff(nil, s)
	}
}

func (c *ACDevice) publishStateDiff(prev, s *ACDeviceState) {
	topicPrefix := c.gw.config.MQTT.TopicPrefix + "/" + c.id

	type update struct {
		id    string
		plug  *bool
		onOff *bool
		speed *int
	}
	fanUpdate := make([]update, 0)

	if isDeviceMultiPort(int(c.Type)) {
		// port changes?
		for i, pi := range s.portInfo {
			portId := "-" + strconv.Itoa(i+1)
			var ppi ACDevicePortInfo
			pf := false
			if prev != nil {
				ppi = prev.portInfo[i]
				pf = ppi.isFan()
			}
			u := update{
				id: portId,
			}

			if pi.isFan() {
				if !pf {
					u.plug = Ptr(true)
				}
				if !pf || ppi.workType != pi.workType || ppi.fan != pi.fan {
					// to handle workType other than 1 (off) and 2 (on)
					// we take the actual fan speed into account
					u.onOff = Ptr((pi.workType == 2 || pi.fan > 0) && pi.workType != 1)
					u.speed = Ptr(int(pi.fan))
				}
			} else if pf || prev == nil {
				// clean up entity
				u.plug = Ptr(false)
			}
			if u.plug != nil || u.onOff != nil || u.speed != nil {
				fanUpdate = append(fanUpdate, u)
			}
		}
	} else if portTypeByResistance(int(s.fanType)) == Fan {
		if prev == nil || prev.workType != s.workType || prev.fan != s.fan {
			fanUpdate = append(fanUpdate, update{
				// to handle workType other than 1 (off) and 2 (on)
				// we take the actual fan speed into account
				onOff: Ptr((s.workType == 2 || s.fan > 0) && s.workType != 1),
				speed: Ptr(int(s.fan)),
			})
		}
	}

	for _, u := range fanUpdate {
		if u.plug != nil {
			if *u.plug {
				c.sendFanDiscoveryMessage(c.id + u.id)
			} else {
				c.clearFanConfig(c.id + u.id)
			}
		}
		if u.onOff != nil {
			state := "OFF"
			if *u.onOff {
				state = "ON"
			}
			c.gw.publish(topicPrefix+u.id+"/on", state)
		}
		if u.speed != nil {
			if *u.speed > 10 {
				log.Warn("Unexpected fan speed: ", *u.speed)
			}
			c.gw.publish(topicPrefix+u.id+"/speed", *u.speed)
		}
	}

	// skip tmp/hum/vpd changes under significance threshold
	// TODO: make threshold configurable
	updTmp := prev == nil || aboveThreshold(prev.tmp, s.tmp, 10)
	updHum := prev == nil || aboveThreshold(prev.hum, s.hum, 10)
	updVpd := prev == nil || aboveThreshold(prev.vpd, s.vpd, 10)

	// TODO: rate-limit mqtt messages?
	// TODO: time-based state dump if nothing changes? for keep-alive purposes
	if updTmp {
		c.gw.publish(topicPrefix+"/tmp", float32(s.tmp)/100.)
	}
	if updHum {
		c.gw.publish(topicPrefix+"/hum", float32(s.hum)/100.)
	}
	if updVpd {
		c.gw.publish(topicPrefix+"/vpd", float32(s.vpd)/100.)
	}
}

func (c *ACDevice) availability() []map[string]string {
	return []map[string]string{
		{"topic": c.gw.config.MQTT.TopicPrefix + "/status"},
		{
			"topic":                 c.gw.config.MQTT.TopicPrefix + "/devices",
			"payload_available":     "True",
			"payload_not_available": "False",
			"value_template":        "{{ '" + c.id + "' in value_json }}",
		},
	}
}

func (c *ACDevice) sendFanDiscoveryMessage(id string) {
	if !c.gw.withHass() {
		return
	}

	topicPrefix := c.gw.config.MQTT.TopicPrefix + "/" + id

	c.gw.publish("homeassistant/fan/"+id+"/config", map[string]interface{}{
		"dev": map[string]string{
			"name":        c.properties["Device Name"],
			"identifiers": c.id,
		},
		"name":        "Fan " + id,
		"uniq_id":     "fan-" + id,
		"stat_t":      topicPrefix + "/on",
		"cmd_t":       topicPrefix + "/set/on",
		"pct_stat_t":  topicPrefix + "/speed",
		"pct_cmd_t":   topicPrefix + "/set/speed",
		"spd_rng_min": 1,
		"spd_rng_max": 10,
		"qos":         0,
		"avty_mode":   "all",
		"avty":        c.availability(),
	})
}

func (c *ACDevice) clearFanConfig(id string) {
	if !c.gw.withHass() {
		return
	}
	// clear out entity just in case there was something there before...
	c.gw.publish("homeassistant/fan/"+id+"/config", "")
}

func (c *ACDevice) sendHassDiscoveryMessage() {
	if !c.gw.withHass() {
		return
	}

	log.Debug("sending homeassistant discovery messages")

	topicPrefix := c.gw.config.MQTT.TopicPrefix + "/" + c.id

	dev := map[string]string{
		"name":         c.properties["Device Name"],
		"manufacturer": "AC Infinity",
		"model":        deviceTypeName(int(c.Type)),
		"identifiers":  c.id,
	}

	avail := c.availability()

	// TODO: figure out which devices have which sensors, and whether the sensors are working properly...
	// TODO: detect sensor loss and mark unavailable or remove entity entirely
	c.gw.publish("homeassistant/sensor/"+c.id+"-tmp"+"/config", map[string]interface{}{
		"uniq_id":                     "tmp-" + c.id,
		"avty_mode":                   "all",
		"avty":                        avail,
		"dev":                         dev,
		"dev_cla":                     "temperature",
		"stat_cla":                    "measurement",
		"unit_of_measurement":         "°C",
		"suggested_display_precision": 1,
		"stat_t":                      topicPrefix + "/tmp",
	})

	c.gw.publish("homeassistant/sensor/"+c.id+"-hum"+"/config", map[string]interface{}{
		"uniq_id":                     "hum-" + c.id,
		"avty_mode":                   "all",
		"avty":                        avail,
		"dev":                         dev,
		"dev_cla":                     "humidity",
		"stat_cla":                    "measurement",
		"unit_of_measurement":         "%",
		"suggested_display_precision": 1,
		"stat_t":                      topicPrefix + "/hum",
	})

	c.gw.publish("homeassistant/sensor/"+c.id+"-vpd"+"/config", map[string]interface{}{
		"uniq_id":             "vpd-" + c.id,
		"avty_mode":           "all",
		"avty":                avail,
		"dev":                 dev,
		"dev_cla":             "pressure",
		"name":                "Vapour-Pressure Deficit",
		"unit_of_measurement": "kPa",
		"stat_t":              topicPrefix + "/vpd",
	})

	if !isDeviceMultiPort(int(c.Type)) {
		// TODO: is there actually a fan... how can we tell for single-port devices?
		c.sendFanDiscoveryMessage(c.id)
	}

	c.sendStateDump()
}

func (c *ACDevice) onCommandMessage(id, cmd string, data []byte) {
	var pkt []byte

	switch cmd {
	case "set/on":
		switch string(data) {
		case "ON":
			pkt = []byte{16, 1, 2}
		case "OFF":
			pkt = []byte{16, 1, 1}
		default:
			log.Warn("unsupported fan state: ", data)
		}
	case "set/speed":
		v, err := strconv.ParseUint(string(data), 10, 8)
		if err != nil || v > 10 {
			log.Warn("invalid fan speed: ", data)
			return
		}
		if v == 0 {
			pkt = []byte{16, 1, 1, 17, 1, byte(v)}
		} else {
			pkt = []byte{16, 1, 2, 18, 1, byte(v)}
		}
	default:
		log.Warn("unknown command: ", cmd)
	}

	if pkt != nil {
		if idx := strings.IndexAny(id, "-"); idx != -1 {
			port, err := strconv.Atoi(id[idx+1:])
			if err == nil && port < int(c.PortCount) {
				pkt = append(pkt, 0xff, byte(port))
			}
		}
		c.SendCommand(cmdPacket(pkt, 3, c.seq))
	}
}

func (c *ACDevice) SendCommand(d []byte) {
	log.Debug("ble command ", d)
	err := c.c.WriteCharacteristic(c.w, d, true)
	if err != nil {
		log.WithError(err).Error("failed to send command")
	}
	c.seq++
}
