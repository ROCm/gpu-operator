#!/bin/bash

PRODUCT_CODES=("7410" "74b5" "74b9") # 74b5 - MI300X, 7410 - MI210, 74b9 - MI325X

for PRODUCT_CODE in "${PRODUCT_CODES[@]}"; do
    COUNTER=0
    DEVICES_PATHS=""

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
            if [ $COUNTER -eq 0 ]; then
                # Unload the VFIO PCI driver for all GPUs
                bash -c "echo 1002 ${PRODUCT_CODE} > /sys/bus/pci/drivers/vfio-pci/remove_id"
                bash -c "echo ${VFIO_DEVICE} > /sys/bus/pci/drivers/vfio-pci/unbind"
            fi
        fi
        DEVICES_PATHS+="path=/sys/bus/pci/devices/$VFIO_DEVICE "
        ((COUNTER++))
        IOMMU_GROUP=$(readlink -f /sys/bus/pci/devices/${VFIO_DEVICE}/iommu_group | awk -F '/' '{print $NF}')
        echo "Group_ID=${IOMMU_GROUP} BUS_ID=${VFIO_DEVICE}"
    done <<< "$LSPCI_OUTPUT"
done