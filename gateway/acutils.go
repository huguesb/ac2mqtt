package gateway

func deviceTypeChar(devType int) string {
	switch devType {
	case 2:
		return "B"
	case 3, 4, 5, 14, 15:
		return "C"
	case 6:
		return "D"
	case 7, 8:
		return "E"
	case 9, 12, 16, 17:
		return "F"
	case 11, 18:
		return "G"
	case 24, 25, 34, 35:
		return "I"
	default:
		return "A"
	}
}

var _devTypeName = map[int]string{
	1:  "Controller 67",
	2:  "Controller 76",
	4:  "Cloudcom B2",
	5:  "Cloudcom B1",
	6:  "Airtap Series",
	7:  "Controller 69",
	8:  "Controller 69 Wi-Fi",
	9:  "Wall Hang Controller",
	11: "Controller 69 PRO",
	12: "Desktop Controller",
	14: "Cloudcom A2",
	15: "Cloudcom A1",
	16: "Wall Hang Controller Pro",
	17: "Desktop Controller Pro",
	18: "Controller 69 PRO+",
	24: "VPD Hygrometer 2",
	25: "VPD Hygrometer 1",
	34: "Wi-Fi VPD Hygrometer 2",
	35: "Wi-Fi VPD Hygrometer 1",
}

func deviceTypeName(devType int) string {
	n, present := _devTypeName[devType]
	if present {
		return n
	}
	return "Controller"
}

func isDeviceMultiPort(dev int) bool {
	switch dev {
	case 7, 8, 9, 11, 12, 16, 17, 18:
		return true
	case 10, 13, 14, 15:
		return false
	default:
		return false
	}
}

func convertPortTypeToLocal(b byte) uint16 {
	switch b {
	case 0x80:
		return 4
	case 1, 0x81:
		return 3
	case 2, 0x82:
		return 6
	case 3, 0x83:
		return 7
	case 4, 0x84:
		return 8
	case 5, 0x85:
		return 5
	case 6, 0x86:
		return 2
	default:
		return 0
	}
}

// reverse-engineered from android app...
func portTypeByResistance(i int) int {
	if 380 <= i && i <= 420 {
		// light
		return 3
	} else if 3135 <= i && i <= 3465 {
		// light
		return 3
	} else if 4845 <= i && i <= 10500 {
		// fan
		return 2
	} else if 10501 <= i && i <= 12600 {
		// humidifier
		return 6
	} else if 12601 <= i && i <= 14385 {
		// dehumidifier
		return 7
	} else if 14386 <= i && i <= 16590 {
		return 4
	} else if 16591 <= i && i <= 18900 {
		return 8
	} else if 18901 <= i && i <= 21525 {
		return 5
	}
	return 0
}

var HEAD = []byte{165, 0}

func addInt16(d []byte, i int, j int) {
	d[i] = byte((j >> 8) & 0xff)
	d[i+1] = byte(j & 0xff)
}

// Command packet format:
//   - 2 bytes: header
//   - 2 bytes: length of command payload
//   - 2 bytes: sequence number
//   - 2 bytes: CRC16 of 6 previous bytes
//   - 1 byte: always 0x00
//   - 1 byte: command category, sample values:
//     -- 1: GET some value
//     -- 3: SET some value
//     -- 8: FACTORY RESET
//   - actual command(s), possibly several in the same category, each with:
//     -- 1 byte ID
//     -- 1 byte payload length
//     -- payload
//   - optionally, for multi-port controllers:
//     -- 1 byte: always 0xff
//     -- 1 byte: port number
//   - 2 bytes: CRC16 of everything after the previous CRC
func cmdPacket(payload []byte, cmdType byte, seq int) []byte {
	d := make([]byte, len(payload)+12)
	copy(d, HEAD)
	addInt16(d, 2, len(payload))
	addInt16(d, 4, seq)
	addInt16(d, 6, int(crc16(d, 0, 6)))
	d[8] = 0
	d[9] = cmdType
	copy(d[10:], payload)
	addInt16(d, len(payload)+10, int(crc16(d, 8, len(payload)+2)))
	return d
}
