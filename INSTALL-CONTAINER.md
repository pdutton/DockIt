# Installing DockIt in a Container

This file documents the steps to build install DockIt on a Linux
host running podman using a remote NFS mount to host the dDockIt
dataset.

## Device Setup (Ubuntu)

```
sudo apt update && sudo apt install nfs-common podman git
```

## Building

Check out github.com/pdutton/DockIt and build the container
from that directory:

```
git clone https://github.com/pdutton/DockIt.git && cd Dockit

VERSION=$(git describe --tags --always --dirty)
sudo podman build --build-arg VERSION=$VERSION -t dockit:$VERSION .
sudo podman tag dockit:$VERSION dockit:latest
```

(i) Replace this with a docker hub location when available.

## Using Quadlets

Quadlets let you set everything up declaratively.

### Create a .volume Quadlet File

Create /etc/containers/systemd/dockit.volume:
```
[Volume]
VolumeName=dockit_volume
Driver=local
Type=nfs
Device=nas2.lan:/nfs/dockit
Options=nfsvers=3,rw
```

### Create a .container File

Create /etc/containers/systemd/dockit.container:
```
[Unit]
Description=DockIt container using NFS volume
After=network-online.target

[Container]
Image=dockit:latest
ContainerName=dockit
Volume=dockit.volume:/data
PublishPort=8080:8080
StopTimeout=15

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

### Apply Changes

```
sudo systemctl daemon-reload
sudo systemctl restart dockit.service
sudo systemctl status dockit.service
```

### View Logs

Using podman:
```
sudo podman logs -f dockit
```

Using Journalctl:
```
sudo journalctl -u dockit.service -f
```


## Using Manual Podman

The old/deprecated way:

### Create a Volume Wrapping the NFS Share

```
sudo podman volume create \
    --driver local \
    --opt type=nfs \
    --opt o=nfsvers=3,rw \
    --opt device=nas2.lan:/nfs/dockit \
    dockit_volume

sudo podman volume inspect dockit_volume
```

### Start the Container

```
sudo podman run --rm -d \
    --name dockit \
    --volume dockit_volume:/data \
    --stop-timeout 15 \
    -p 8080:8080 \
    dockit:latest
```

### Stop the Container

```
sudo podman stop dockit
```

### Ensuring the Container Automatically Starts/Stops on Reboot

```
sudo podman generate systemd --name --new --files dockit
sudo mv container-dockit.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable container-dockit.service
sudo systemctl start container-dockit.service
sudo systemctl status container-dockit.service
```


