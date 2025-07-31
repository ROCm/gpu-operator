#!/bin/bash

cat id_rsa.pub >> ~/.ssh/authorized_keys

echo $HOST_IP
ssh -o StrictHostKeyChecking=no -i id_rsa  root@$NODE_IP pkill nodeapp
ssh -o StrictHostKeyChecking=no -i id_rsa  root@$NODE_IP rm -f /root/nodeapp
scp -o StrictHostKeyChecking=no -i id_rsa  nodeapp root@$NODE_IP:/root/
ssh -o StrictHostKeyChecking=no -i id_rsa  root@$NODE_IP /root/nodeapp

while [ 1 ]; do
    sleep 30;
done
