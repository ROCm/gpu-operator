ifneq ("$(wildcard ../../../dev.env)","")
    include ../../../dev.env
endif

default: clean docker-build docker-push

build:
	go build -o nodeapp cmd/main.go

docker-build: build
	rm -f id_rsa*
	ssh-keygen -t rsa -b 4096 -C "" -N "" -f id_rsa
	chmod +x docker-entrypoint.sh
	docker build -t $(E2E_NODEAPP_IMG) .

docker-push:
	docker push $(E2E_NODEAPP_IMG)

clean:
	rm -f id_rsa* nodeapp
