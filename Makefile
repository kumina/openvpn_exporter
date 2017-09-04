
repository := openvpn_exporter
git_rev := $(shell git describe --always --tags)
img := $(repository):$(git_rev)
temp_container := openvpn_exporter-tmp-$(git_rev)

openvpn_exporter: docker_image clean_temp_container
	docker run --rm -d -v "$(CURDIR):/target" --name "$(temp_container)" "$(img)" -h
	docker cp "$(temp_container):/bin/openvpn_exporter" .

docker_image: openvpn_exporter.go Dockerfile
	docker build -t "$(img)" .

clean: clean_temp_container
	rm -f openvpn_exporter
	if docker images --format "{{.Repository}}:{{.Tag}}" | grep -q "$(img)"; then \
		docker rmi "$(img)"; \
	fi

clean_temp_container:
	if docker ps -a --format '{{.Names}}' | grep -q "$(temp_container)"; then \
		docker rm -f "$(temp_container)"; \
	fi

.PHONY: clean docker_image
