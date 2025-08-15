#!/bin/bash

NS=kube-amd-gpu
METRICS_SERVICE=$(kubectl get svc -n ${NS} -o name | grep metrics-exporter)
NODEPORT=$(kubectl get -n ${NS} ${METRICS_SERVICE} -o jsonpath='{.spec.ports[*].nodePort}' | tr ' ' '\n' | sort -u)
NODEIPS=$(kubectl get pods -n ${NS} -l "app.kubernetes.io/name=metrics-exporter" -o jsonpath='{$.items[*].status.hostIP}' | tr ' ' '\n' | sort -u)
IPPORT=""

for ip in ${NODEIPS}; do
	IPPORT+=" '${ip}:${NODEPORT}' "
done

TARGETS=$(echo ${IPPORT} | tr ' ' ',')
GRAFANA_IP=$(ip route get 1.2.3.4 | awk '{print $7}')
echo "found targets $TARGETS"
cat <<EOF >/tmp/prometheus.yml
global:
  scrape_interval:     5s # By default, scrape targets every 15 seconds.

  # Attach these labels to any time series or alerts when communicating with
  # external systems (federation, remote storage, Alertmanager).
  external_labels:
    monitor: 'amdgpu-monitor'

# A scrape configuration containing exactly one endpoint to scrape:
# Here it's Prometheus itself.
scrape_configs:
  # The job name is added as a label \`job=<job_name>\` to any timeseries scraped from this config.
  - job_name: 'prometheus'

    # Override the global default and scrape targets from this job every 5 seconds.
    scrape_interval: 5s

    static_configs:
      - targets: [${TARGETS}]
EOF

docker port prometheus >/dev/null 2>&1 && docker rm -f prometheus
docker port grafana >/dev/null 2>&1 && docker rm -f grafana
docker run --rm -d --name prometheus -p 9090:9090 -v /tmp/prometheus.yml:/etc/prometheus/prometheus.yml prom/prometheus
docker run --rm -d --name grafana -p 3000:3000 grafana/grafana:latest
sleep 5
cat <<EOF >/tmp/prom.json
{
  "name":"prometheus", 
  "type":"prometheus", 
  "typeName": "Prometheus", 
  "url":"http://${GRAFANA_IP}:9090", 
  "access":"proxy", 
  "user": "", 
  "database": "", 
  "basicAuth":false, 
  "isDefault": false
}

EOF
curl -s -u "admin:admin" -XPOST -H "Content-Type:application/json" -H "Accept: application/json" -d@/tmp/prom.json http://${GRAFANA_IP}:3000/api/datasources >/dev/null

echo "prometheus started http://${GRAFANA_IP}:9090"
echo "grafana started http://${GRAFANA_IP}:3000"
