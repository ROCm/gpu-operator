#!/usr/bin/env bash
set -x
set -euo pipefail

# Graceful shutdown for dockerd if needed
term() {
    killall dockerd || true
    wait
}

# Optionally run Docker in the container if you need nested Docker
#sudo dockerd -s vfs &
trap term INT TERM
dockerd -s vfs &

# If your build or environment requires this sysctl tweak
sudo sysctl -w vm.max_map_count=262144 || true

if [[ -n "${USER_NAME:-}" && -n "${USER_UID:-}" && -n "${USER_GID:-}" ]]; then
	echo "Creating user ${USER_NAME} with UID=${USER_UID}, GID=${USER_GID}..."

	if ! getent group "$USER_GID" >/dev/null; then
		groupadd -g "$USER_GID" "$USER_NAME"
	fi

	if ! id -u "$USER_NAME" >/dev/null 2>&1; then
		useradd -m -u "$USER_UID" -g "$USER_GID" -s /bin/bash "$USER_NAME"
		echo "$USER_NAME ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$USER_NAME
		chmod 0440 /etc/sudoers.d/$USER_NAME
	fi

	if ! getent group docker >/dev/null; then
		groupadd -g 999 docker
	fi
	usermod -aG docker "$USER_NAME"

	chown -R "$USER_UID":"$USER_GID" /gpu-operator
	chown -R "$USER_UID":"$USER_GID" /home/$USER_NAME

	su - "$USER_NAME" -c "echo 'export GOPATH=/home/$USER_NAME/go' >> ~/.bashrc"
	su - "$USER_NAME" -c "echo 'export PATH=\$GOPATH/bin:/usr/local/go/bin:\$PATH' >> ~/.bashrc"
	su - "$USER_NAME" -c "echo 'export PATH=/usr/local/go/bin:\$PATH' >> ~/.bashrc"
	exec su - "$USER_NAME" -c "$@"
else
	echo "Running as default user (root)..."
	exec "$@"
fi
