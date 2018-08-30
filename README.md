# What is the Chef Waiter
The Chef Waiter service is used to create on demnad runs of chef without the use of push jobs.
Push jobs are not available when using the managed chef server service from opscode.

# How do I use Chef Waiter
The following URLs are available to you.
The are desinged to be used by API clients rather than Web browsers. They return JSON strings mostly or simple text.

The default port for running Chef Waiter on is 8901 TCP.

Example request:
```bash
$> curl http://127.0.0.1:8901/chefclient
$> {"35434398-b40a-4686-ab38-38deccd4241b":{"status":"registered","exitcode":99,"starttime":1501605431834067960,"ondemand":true}}
$> curl http://127.0.0.1:8901/chefclient/35434398-b40a-4686-ab38-38deccd4241b
$> {"35434398-b40a-4686-ab38-38deccd4241b":{"status":"complete","exitcode":0,"starttime":1501605431834067960,"ondemand":true}}
$> curl http://127.0.0.1:8901/chef/lastrun
$> {"last_run_guid":"35434398-b40a-4686-ab38-38deccd4241b"}
```

Chefwaiter will determin if the chef run passed or failed based on the exit code of the run. If the run passed you will see a status of `complete` if it failed you will see `failed`.

Below is a table describing the API for chef waiter. Chefwaiter was built with easy intergration for humans in mind. Therefore all the requests are GET based. There is very little that chefwaiter needs in terms of data and these are passed in via the URL. These are also described below:

| URL | Description|
|-----|------------|
| /chefclient | Use this to create a run. You will have a json payload returned with a guid for the run. |
| /chefclient/{guid} | Used with the GUID that you received from /chefclient to get the status of the run.
| /cheflogs/{guid} | Used with the GUID that you received from /chefclient to get the chef logs from a run.
| /chef/nextrun | Used to get the time when the next run will happen. This time is the time when the server is free to start the next run and will usually happen with in a minute of this time.
|/chef/interval| Used to get the time between automatic chef runs.
|/chef/interval/{i}| Used to set the time between chef runs. This needs to be a positive number and represents minutes between runs.
|/chef/on| Used to turn on automatic runs of chef
|/chef/off| Used to turn off automatic runs of chef
|/chef/lastrun| Returns the guid of the last run. It starts as blank when the service starts.
|/chef/enabled| Used to check if chef is currently enabled to run periodically
|/chef/maintenance| Shows if the chef waiter is in maintenance mode currently.
|/chef/maintenance/start/{i}| Requests that chef waiter be put into maintenance mode for i number of minutes. This must be a whole number.
|/chef/maintenance/end| Removes the maintenance timer allowing periodic runs to start again.
|/chef/lock| Shows the status of the lock for runs.
|/chef/lock/set| Turns on the lock for chef runs. Stops any runs from occurring.
|/chef/lock/remove| Turns off the lock for chef runs. Enables normal operation again.
|/_status | Returns a epoch time from the time that the server was started. It can be used to infer a restart.
| /healthcheck | Returns a 200 OK to show that the server is online.

# Installing
Preferred option: The chef waiter can be installed via the nvm_chefwaiter cookbook.

Optional method: Copy the binary to your computer and from a terminal or prompt windows run 
```
# Linux
/usr/local/bin/chefwaiter --service install

# Windows
C:\Program Files\chefwaiter\chefwaiter.exe --service install
```

Remember to allow **8901 TCP** Inbound if you choose to install manually.

# Running
The service is runs the same in Windows and Linux. The service binary itself is responsible for creating the service files needed to start and run the chef waiter as a service.

Make sure that the service is running after installing it as discussed in the _Installing_ section.
The service will need port **8901-TCP** open to communicate with the outside world.

| Port | Protocol | Discription |
|----|--------|-----------|
| 8901 | TCP | Used to host the HTTP service for Chef Waiter |

|Directory|OS|Discription|
|---------|--|-----------|
|/etc/chefwaiter/ | Linux | Used to store configuration and state files for Chef Waiter|
|/usr/local/bin/ | Linux | Location the binary is stored on a linux computer|
|/var/log/chefwaiter/ |Linux| Location where Chef Waiter will store the log files for chef|
|C:\Program Files\chefwaiter\ | Windows | Location of both configuration files and binary|
|C:\logs\chefwaiter\ |Windows| Location where Chef Waiter will store the log files for chef|

The Chef Waiter can be configured by a configuration file in the form of json.
The file location needs to be set using an environment variable "CHEFWAITER_CONFIG".
eg. /etc/chefwaiter/config.json | c:\Program Files\chefwaiter\config.json
If no config file is specified Chef Waiter will start with sane defaults.

An example file is below:
```json
{
    "state_table_size": 20,
    "periodic_chef_runs": true,
    "run_interval": 10,
    "debug": false,
    "logs_location": /var/log/chefwaiter,
    "state_location": /etc/chefwaiter
}
```
Default settings:
| Setting | Windows | Linux | Description |
|---|---|---|---|
|state_table_size| 20 | 20 | Chefwaiter will keep a log of the past x number of run. This setting dictates that value. |
| periodic_chef_runs | true | true | This setting will tell chef waiter to run chef runs periodically like the normal chef service. |
| run_interval | 30 | 30 | How often in minutes should chef waiter start a chef run. |
| debug | false | false | Show debug log printing. |
| logs_location | C:\logs\chefwaiter | /var/log/chefwaiter | Where should chefwaiter store the chef run logs. |
| state_location | C:\Program Files\chefwaiter | /etc/chefwaiter | Chefwaiter writes a state file to disk periodically to maintain state through reboots. This settings dictates where that file should be kept. |
| enable_tls | false | false | Should Chefwaiter us TLS on the web server. |
| certificate_path | ./cert.crt | ./cert.crt | location of the TLS certificate. |
| key_path | ./cert.key | ./cert.key | Location of the TLS certifiates private key. |

## Maintenance mode

The Chef Waiter can be put into maintenance mode. This dictates that no periodic runs will be allowed to be triggered during this time period. Therefore any triggered runs will be skipped and you would have to wait for the next trigger to be released.
Maintenance mode has no effect to "on demand" runs. This will allow you to control the runs but also to stop uncontrolled runs from occuring while you are doing deployments.

## Locking the chef waiter

Chef waiter has a lock in it. This allows you to request that a server not run chef on demand or periodically. This is useful for servers
that are in production that you wish to protect from accidental changes.
Use /chef/lock for checkting the status of the lock. Set and Remove will enable and disable the lock.

## Chef service replacement

The Chef Waiter has been written to be a replacement for the chef service. This feature is turned on by default and can be turn off with the use of the "periodic_chef_runs" configuration file setting. By making of this feature you will be able to get the logs for the periodic runs via the API. Periodic runs will run before on demand runs should there be 2 that are ready to be kicked off at the same time. This in turn means that your on demand runs will always return the latest details.

The periodic runs can also be controlled via the API. You can change the interval in minutes for the runs as well as turn it off and on. You can use "/chef/lastrun" to get the GUID for the last run which in turn will allow you to get the logs for the run.

The logs for the periodic runs are stored under the same directory as on demand runs. They are also subject to the same clean up process. This means that you do not need to rotate logs are the chef waiter will do that for you.

## Flow

1. /chefclient (gather the GUID from this step)
1. /chefclient/<guid from step 1>
1. /cheflogs/<guid from step 1>

If you request a run while a run is queued you will keep getting the same guid back until the run starts and a new run can be queued. This means that you can only ever have 1 chef run running and 1 queued at a time.

# Logging
The service will log to the default logging system for the OS that it is running on. Either Windows Event Viewer or Syslog for linux.

Logs for chef runs will be contained in a file that has the name of the GUID that represents the chef run. The files will be cleared out by the chef waiter as the state table internally is updated. This is triggered every minute but is contolled by a flag to specify the number of state files that you want to keep. The default is 20.
This can be changed as well as the location of the logs by settings in the above configuration file.

Log files will look like below:

* /var/log/chefwaiter/0038cf85-68a1-4b8a-8898-f56261f02d65.log
* C:\logs\chefwaiter\0038cf85-68a1-4b8a-8898-f56261f02d65.log