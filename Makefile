.PHONY: app

app:
	go build -o app
	sudo setcap 'cap_net_bind_service=+ep' app
