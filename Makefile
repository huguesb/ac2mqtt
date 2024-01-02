build:
	go build -o ac2mqtt cmd/ac2mqtt/main.go

install: build
	sudo install -m 755 ac2mqtt /usr/bin/
	sudo install -d /etc/ac2mqtt

systemd-install:
	sudo install -m 644 service/ac2mqtt.service /lib/systemd/system/ac2mqtt.service
	sudo systemctl enable ac2mqtt.service
	@echo "Please install a configuration file to /etc/ac2mqtt/config.yml, because"
	@echo "the systemd service uses it from there. You can start the service manually with:"
	@echo "    systemctl start ac2mqtt.service"

clean:
	rm -f ac2mqtt

all: build install systemd-install
