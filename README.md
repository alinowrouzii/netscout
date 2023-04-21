# network-status-checker
A checker that checks the network availability of some addresses inside one host

**Usage:** When you want to know inside one remote host that access to which hosts are available and which are unavailable. 
In some intervals e.g. 1second, it tries to open a TCP connection for a given host with timeout=500ms.
If dialing was successful then the successful record type will be saved in the redis. If not, the unsuccessful record type will be saved.
I used a redisTimeSeries to store the time series record.

**Grafana usage:**
The app contains the `/`, `/search`, `/query` and `/annotations` endpoints and therefore is compatible with the simpleJson plugin and can be used
in grafana with that plugin.

**To run the app:**
```
go build
./network-status-checker --config config.yml
```

## Deploy using ansible
```
cd ansible
ansible-galaxy collection install community.docker
ansible-playbook -i inventory.yml playbook.yml -k -K
```

#### TODO: add health-check to containers