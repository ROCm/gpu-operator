#!/bin/bash

ssh -o StrictHostKeyChecking=no -i id_rsa  root@$NODE_IP pkill nodeapp
ssh -o StrictHostKeyChecking=no -i id_rsa  root@$NODE_IP rm -f /root/nodeapp
sed -i '$d' /root/.ssh/authorized_keys
