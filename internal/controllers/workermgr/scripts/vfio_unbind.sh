#!/bin/bash

PRODUCT_CODES=($$PCI_DEVICE_ID_LIST)

for PRODUCT_CODE in "${PRODUCT_CODES[@]}"; do
    # Load VFIO PCI driver on GPU VF devices, if not done already
    LSPCI_OUTPUT=$(lspci -nn -d 1002:${PRODUCT_CODE})

    # Check if LSPCI_OUTPUT is empty
    if [ -z "$LSPCI_OUTPUT" ]; then
        continue
    fi

    while IFS= read -r LINE; do
        PCI_ADDRESS=$(echo "$LINE" | awk '{print $1}')
        VFIO_DRIVER=$(lspci -k -s "$PCI_ADDRESS" | grep -i vfio-pci | awk '{print $5}')
        VFIO_DEVICE="0000:$PCI_ADDRESS"
        if [ "$VFIO_DRIVER" == "vfio-pci" ]; then
            # Unload the VFIO PCI device
            bash -c "echo ${VFIO_DEVICE} > /sys/bus/pci/drivers/vfio-pci/unbind"
        fi
        IOMMU_GROUP=$(readlink -f /sys/bus/pci/devices/${VFIO_DEVICE}/iommu_group | awk -F '/' '{print $NF}')
        echo "Group_ID=${IOMMU_GROUP} BUS_ID=${VFIO_DEVICE}"
    done <<< "$LSPCI_OUTPUT"
done