# DCM Systemd Integration

## Background 

The Device Config Manager (DCM) orchestrates hardware-level tasks such as GPU partitioning. Before initiating partitioning, it gracefully stops specific systemd services defined in a configmap to prevent any processes (gpuagent, etc) from partition interference and ensure consistent device states

## K8S ConfigMap enhancement

The configmap contains a key "gpuClientSystemdServices" which declares the list of services to manage: 

```yaml
"gpuClientSystemdServices": {
    "names": ["amd-metrics-exporter", "gpuagent"] 
}
```
- These are the unit names (without the. service suffix) of systemd services related to GPU runtime agents. We add the suffix as a part of the code
- Users can add/modify services to the above list 

## ConfigMap

```yaml  
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-manager-config
  namespace: kube-amd-gpu
data:
  config.json: |
    {
      "gpu-config-profiles":
      {
          "cpx-profile":
          {
              "skippedGPUs": {
                  "ids": []
              },
              "profiles": [
                  {
                      "computePartition": "CPX",
                      "memoryPartition": "NPS4",
                      "numGPUsAssigned": 8
                  }
              ]
          },
          "spx-profile":
          {
              "skippedGPUs": {
                  "ids": []
              },
              "profiles": [
                  {
                      "computePartition": "SPX",
                      "memoryPartition": "NPS1",
                      "numGPUsAssigned": 8
                  }
              ]
          }
      },
      "gpuClientSystemdServices": {
           "names": ["amd-metrics-exporter", "gpuagent"]
       }
    }
```

## Required Mounts for D-Bus & systemd Integration

| **Mount Name**         | **Mount Path**        | **Purpose**                                                              |
|------------------------|------------------------|---------------------------------------------------------------------------|
| `etc-systemd`          | `/etc/systemd`         | Access unit files for service definitions                                |
| `run-systemd`          | `/run/systemd`         | Enables access to systemd runtime state                                  |
| `usr-lib-systemd`      | `/usr/lib/systemd`     | Required for systemd libraries and binaries                              |
| `var-run-dbus`         | `/var/run/dbus`        | Allows DCM to communicate via system D-Bus (`system_bus_socket`)         |

## Workflow

- DCM uses D-Bus APIs to query, stop, and restart systemd services programmatically, ensuring precise service orchestration. 

- Extract Service List: On startup, DCM parses the configmap and retrieves the names array under gpuClientSystemdServices. Each entry is appended with (. service) to form full unit names. 

- Capture Pre-State:
    - For each service: 
        - It checks status using D-Bus via `org.freedesktop.systemd1.Manager.GetUnit.` 
        - Stores current state (e.g. `active`, `inactive`, `not-loaded`) in PreStateDB. 
        - This DB is used for restoring service state post-partitioning. 

- Stop Services: Services are stopped gracefully using D-Bus APIs. This ensures they release GPU resources and don't disrupt the partitioning operation. We check if the service is present before stopping it using the CheckUnitStatus API. 

- Perform Partitioning: Once services are stopped temporarily, DCM initiates the partitioning logic (using node labels/configmap profiles) and completes the partitioning workflow 

- Restart & Restore State After partitioning: 
    - DCM checks PreStateDB to determine which services were previously active. 
    - Only those Services are restarted accordingly using the D-Bus invocation APIs. 
    - Additionally, PreStateDB is cleared via a CleanupPreState() function to reset the tracker DB for the next run. 

# Conclusion 

- Avoids GPU contention during partitioning (device-busy errors aren’t seen during partition) 
- Maintains service continuity with minimal downtime 