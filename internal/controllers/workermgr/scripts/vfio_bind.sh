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
        if [ "$VFIO_DRIVER" != "vfio-pci" ]; then
            if [ $COUNTER -eq 0 ]; then
                # Load the VFIO PCI driver for all GPUs
                modprobe vfio_iommu_type1 allow_unsafe_interrupts
                modprobe vfio_pci disable_idle_d3=1
                bash -c "echo 1 > /sys/module/vfio_iommu_type1/parameters/allow_unsafe_interrupts"
                bash -c "echo 1002 ${PRODUCT_CODE} > /sys/bus/pci/drivers/vfio-pci/new_id"
            fi
        fi
        # Check if IOMMU entry found for each GPU (VFIO device)
        IOMMU_GROUP=$(readlink -f /sys/bus/pci/devices/${VFIO_DEVICE}/iommu_group | awk -F '/' '{print $NF}')
        if [ -e "/dev/vfio/$IOMMU_GROUP" ]; then
            chown "$UID:$UID" /dev/vfio/$IOMMU_GROUP
        else
            echo "Error: IOMMU entry not found for GPU VF Device: $VFIO_DEVICE, IOMMU Group: $IOMMU_GROUP"
            exit 1
        fi
        DEVICES_PATHS+="path=/sys/bus/pci/devices/$VFIO_DEVICE "
        ((COUNTER++))
        echo "Group_ID=${IOMMU_GROUP} BUS_ID=${VFIO_DEVICE}"
    done <<< "$LSPCI_OUTPUT"
done