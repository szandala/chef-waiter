package webengine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/morfien101/chef-waiter/cheflogs"
	"github.com/morfien101/chef-waiter/chefrunner"
	"github.com/morfien101/chef-waiter/internalstate"
	"github.com/morfien101/chef-waiter/logs"

	"github.com/gorilla/mux"
)

type customRunWhitelist struct {
	whitelist []string
	use       bool
}

// HTTPEngine holds all the requires types and functions for the API to work.
type HTTPEngine struct {
	router         *mux.Router
	logger         logs.SysLogger
	state          internalstate.StateTableReadWriter
	appState       internalstate.AppStatusReader
	worker         chefrunner.Worker
	chefLogsWorker cheflogs.WorkerReader
	server         *http.Server
	whitelists     *customRunWhitelist
}

// New returns a struct that holds the required details for the API engine.
// You still need to start it with StartHTTPEngine()
func New(
	state internalstate.StateTableReadWriter,
	appState internalstate.AppStatusReader,
	worker chefrunner.Worker,
	chefLogsWorker cheflogs.WorkerReader,
	logger logs.SysLogger,
) (e *HTTPEngine) {
	httpEngine := &HTTPEngine{
		logger:         logger,
		state:          state,
		appState:       appState,
		worker:         worker,
		chefLogsWorker: chefLogsWorker,
		router:         mux.NewRouter(),
		whitelists:     &customRunWhitelist{whitelist: []string{}},
	}

	httpEngine.router.HandleFunc("/chefclient", httpEngine.registerChefRun).Methods("Get")
	httpEngine.router.HandleFunc("/chefclient", httpEngine.registerChefCustomRun).Methods("Post")
	httpEngine.router.HandleFunc("/chefclient/{guid}", httpEngine.getChefStatus).Methods("Get")
	httpEngine.router.HandleFunc("/cheflogs/{guid}", httpEngine.getChefLogs).Methods("Get")
	httpEngine.router.HandleFunc("/chef/nextrun", httpEngine.getNextChefRun).Methods("Get")
	httpEngine.router.HandleFunc("/chef/interval", httpEngine.getChefRunInterval).Methods("Get")
	httpEngine.router.HandleFunc("/chef/interval/{i}", httpEngine.setChefRunInterval).Methods("Get")
	httpEngine.router.HandleFunc("/chef/on", httpEngine.setChefRunEnabled).Methods("Get")
	httpEngine.router.HandleFunc("/chef/off", httpEngine.setChefRunDisabled).Methods("Get")
	httpEngine.router.HandleFunc("/chef/lastrun", httpEngine.getLastRunGUID).Methods("Get")
	httpEngine.router.HandleFunc("/chef/allruns", httpEngine.getAllRuns).Methods("Get")
	httpEngine.router.HandleFunc("/chef/enabled", httpEngine.getChefPeridoicRunStatus).Methods("Get")
	httpEngine.router.HandleFunc("/chef/maintenance", httpEngine.getChefMaintenance).Methods("Get")
	httpEngine.router.HandleFunc("/chef/maintenance/start/{i}", httpEngine.setChefMaintenance).Methods("Get")
	httpEngine.router.HandleFunc("/chef/maintenance/end", httpEngine.removeChefMaintenance).Methods("Get")
	httpEngine.router.HandleFunc("/chef/lock", httpEngine.getChefLock).Methods("Get")
	httpEngine.router.HandleFunc("/chef/lock/set", httpEngine.setChefLock).Methods("Get")
	httpEngine.router.HandleFunc("/chef/lock/remove", httpEngine.removeChefLock).Methods("Get")
	httpEngine.router.HandleFunc("/status", httpEngine.getStatus).Methods("Get")
	httpEngine.router.HandleFunc("/_status", httpEngine.getStatus).Methods("Get")
	httpEngine.router.HandleFunc("/healthcheck", httpEngine.healthCheck).Methods("Get")

	return httpEngine
}

// SetWhitelist is used to tell the server what custom runs are allowed.
func (e *HTTPEngine) SetWhitelist(whitelist []string) {
	e.whitelists.whitelist = whitelist
	e.whitelists.use = true
}

// StartHTTPEngine will start the web server in a nonTLS mode.
// It also requires that the listening address be passes in as a string.
// Should be used in a go routine.
func (e *HTTPEngine) StartHTTPEngine(listenerAddress string) error {
	// Start the HTTP Engine
	e.server = &http.Server{Addr: listenerAddress, Handler: e.router}
	return e.server.ListenAndServe()
}

// StartHTTPSEngine will start the web server with TLS support using the given cert and key values.
// It also requires that the listening address be passes in as a string.
// Should be used in a go routine.
func (e *HTTPEngine) StartHTTPSEngine(listenerAddress, certPath, keyPath string) error {
	// Start the HTTP Engine
	e.server = &http.Server{Addr: listenerAddress, Handler: e.router}
	return e.server.ListenAndServeTLS(certPath, keyPath)
}

// StopHTTPEngine will stop the web server grafefully.
// It will give the server 5 seconds before just terminating it.
func (e *HTTPEngine) StopHTTPEngine() error {
	// Stop the HTTP Engine
	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	return e.server.Shutdown(ctx)
}

// ServeHTTP is used to allow the router to start accepting requests before the start is started up. This will help with testing.
func (e *HTTPEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.router.ServeHTTP(w, r)
}

func setContentJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
}

func jsonMarshal(x interface{}) ([]byte, error) {
	return json.MarshalIndent(x, "", "  ")
}

func printJSON(w http.ResponseWriter, jsonbytes []byte) (int, error) {
	return fmt.Fprint(w, string(jsonbytes), "\n")
}

// RegisterChefRun is called to run chef on the server.
func (e *HTTPEngine) registerChefRun(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	if e.state.ReadRunLock() {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "{\"Error\":\"Chefwaiter is locked\"}\n")
		return
	}
	guid := e.worker.OnDemandRun()
	logs.DebugMessage(fmt.Sprintf("registerChefRun() - %s", guid))
	state := e.state.Read(guid)
	jsonBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "{\"Error\":\"Failed to read guid status\"}\n")
		return
	}
	printJSON(w, jsonBytes)
}

func (e *HTTPEngine) registerChefCustomRun(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)

	checklock := true

	// Check if the server is locked unless we have an override URL parameter available.
	if value, ok := r.URL.Query()["force"]; ok {
		if value[0] == "true" {
			checklock = false
			logs.DebugMessage(fmt.Sprintln("registerChefCustomRun() running regardless of lock."))
			e.logger.Infof("Running a custom job regardless of lock from %s\n", r.RemoteAddr)
		}
	}

	if checklock {
		if e.state.ReadRunLock() {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "{\"Error\":\"Chefwaiter is locked\"}\n")
			return
		}
	}

	defer r.Body.Close()
	bodySlurp := make([]byte, 513)
	n, err := r.Body.Read(bodySlurp)
	if err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		e.logger.Errorf("Request to custom job failed while reading the body. Error: %s", err)
		return
	}
	if n > 512 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "{\"Error\":\"Body sent is too large. Max size 512 bytes\"}\n")
		return
	}
	customRunText := string(bytes.TrimRight(bodySlurp, "\x00"))
	if e.whitelists.use {
		matched := false
		for _, whitelistText := range e.whitelists.whitelist {
			if customRunText == whitelistText {
				matched = true
				break
			}
		}
		if !matched {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "{\"Error\":\"Whitelist does not contain '%s'\"}\n", customRunText)
			return
		}
	}
	guid := e.worker.CustomRun(customRunText)
	logs.DebugMessage(fmt.Sprintf("registerChefCustomRun() - %s", guid))
	jsonbytes, err := jsonMarshal(e.state.Read(guid))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "{\"Error\":\"Failed to read guid status\"}\n")
		return
	}
	printJSON(w, jsonbytes)
}

// GetChefStatus - writes the state of the requested guid.
func (e *HTTPEngine) getChefStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	logs.DebugMessage(fmt.Sprintf("getChefStatus() - %s", vars["guid"]))
	setContentJSON(w)
	status := e.state.Read(vars["guid"])
	jsonBytes, err := jsonMarshal(status)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "{\"Error\":\"Failed to read guid status\"}\n")
		return
	}
	printJSON(w, jsonBytes)
}

// GetStatus - Writes the applications internal status in json to the http writer.
func (e *HTTPEngine) getStatus(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	state, err := e.appState.JSONEncoded()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	w.Write(state)
	fmt.Fprint(w, "\n")
}

// HealthCheck - Writes a HealthCheck message that can be used to check the state
// of the chef waiter.
func (e *HTTPEngine) healthCheck(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	fmt.Fprint(w, "{\"state\": \"OK\"}")
}

// getChefLogs - is responsible for displaying the chef logs that have been created
// by a chef run.
func (e *HTTPEngine) getChefLogs(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// Set the content type
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// We first need to look for the log file.
	// Throw a 404 if the file is not there
	if err := e.chefLogsWorker.IsLogAvailable(vars["guid"]); err != nil {
		w.WriteHeader(http.StatusNotFound)
		logs.DebugMessage(fmt.Sprintf("Unavailable: %s, %s", e.chefLogsWorker.GetLogPath(vars["guid"]), err))
		fmt.Fprintf(w, "404 - %s not found\n", vars["guid"])
		return
	}
	logs.DebugMessage(fmt.Sprintf("Found: %s", e.chefLogsWorker.GetLogPath(vars["guid"])))

	// If it is there then we need to read it out.
	file, err := os.Open(e.chefLogsWorker.GetLogPath(vars["guid"]))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		e.logger.Errorf("Failed to open %s: %v", e.chefLogsWorker.GetLogPath(vars["guid"]), err)
		return
	}
	// remember to close it at the end.
	defer file.Close()

	// At this point we are about to read out the file so it is safe to
	// write the headers for OK Status.
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fmt.Fprintln(w, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		e.logger.Errorf("Failed to read file: %s, Error: %s", file.Name(), err)
	}
}

func (e *HTTPEngine) getNextChefRun(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	w.WriteHeader(http.StatusOK)
	// json string with epoch and string time
	epoch := e.state.GetlastRunStartTime() + e.state.ReadChefRunTimer()
	next := &struct {
		Epoch int64  `json:"epoch"`
		Str   string `json:"human"`
	}{
		Epoch: epoch,
		Str:   time.Unix(epoch, 0).String(),
	}
	json.NewEncoder(w).Encode(next)
}

func (e *HTTPEngine) setChefRunInterval(w http.ResponseWriter, r *http.Request) {
	// check if the string is a number and is positive
	setContentJSON(w)
	vars := mux.Vars(r)
	i, err := strconv.Atoi(vars["i"])
	if err != nil || i < 0 {
		e.logger.Errorf("/chef/interval/%s is not a positive number", vars["i"])
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "{\"Error\":\"Only a positive number will be accepted\"}\n")
		return
	}
	if i <= 0 {
		e.logger.Errorf("/chef/interval/%s is not a positive number", vars["i"])
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "{\"Error\":\"Only a positive number will be accepted\"}\n")
		return
	}

	e.state.WriteChefRunTimer(int64(i))
}

func (e *HTTPEngine) getChefRunInterval(w http.ResponseWriter, r *http.Request) {
	i := e.state.ReadChefRunTimer()
	setContentJSON(w)
	fmt.Fprintf(w, "{\"current_interval\":\"%d minutes\"}\n", i/60)
}

// setChefRunEnabled - enables periodic runs
func (e *HTTPEngine) setChefRunEnabled(w http.ResponseWriter, r *http.Request) {
	e.state.WritePeriodicRuns(true)
	setContentJSON(w)
	fmt.Fprintf(w, "{\"chef_runs_enabled\":%v}\n", e.state.ReadPeriodicRuns())
}

// setChefRunDisabled - disables periodic runs
func (e *HTTPEngine) setChefRunDisabled(w http.ResponseWriter, r *http.Request) {
	e.state.WritePeriodicRuns(false)
	setContentJSON(w)
	fmt.Fprintf(w, "{\"chef_runs_enabled\":%v}\n", e.state.ReadPeriodicRuns())
}

// getChefPeridoicRunStatus - returns details about if periodic runs are enabled.
func (e *HTTPEngine) getChefPeridoicRunStatus(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	fmt.Fprintf(w, "{\"chef_runs_enabled\":%v}\n", e.state.ReadPeriodicRuns())
}

func (e *HTTPEngine) getLastRunGUID(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	fmt.Fprintf(w, "{\"last_run_guid\":\"%s\"}\n", e.state.ReadLastRunGUID())
}

func (e *HTTPEngine) getAllRuns(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	jobs := e.state.ReadAllJobs()

	jsonJobs, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "{\"Error\":\"Failed to gather jobs\"}\n")
		return
	}
	fmt.Fprint(w, string(jsonJobs), "\n")
}

func (e *HTTPEngine) getChefMaintenance(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	fmt.Fprintf(w, "{\"end_time\":\"%s\", \"in_maintenance\":%v}\n", time.Unix(e.state.ReadMaintenanceTimeEnd(), 0), e.state.InMaintenceMode())
}
func (e *HTTPEngine) setChefMaintenance(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)

	vars := mux.Vars(r)
	minutes, err := strconv.Atoi(vars["i"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	endTime := time.Now().Unix() + int64(minutes*60)
	e.state.WriteMaintenanceTimeEnd(endTime)
	fmt.Fprintf(w, "{\"end_time\":\"%s\"}\n", time.Unix(endTime, 0))
}

func (e *HTTPEngine) removeChefMaintenance(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)

	e.state.WriteMaintenanceTimeEnd(0)
	fmt.Fprintf(w, "{\"end_time\":\"%s\"}\n", time.Unix(e.state.ReadMaintenanceTimeEnd(), 0))
}

func (e *HTTPEngine) getChefLock(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	fmt.Fprintf(w, "{\"Locked\": %t}\n", e.state.ReadRunLock())
}

func (e *HTTPEngine) setChefLock(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	e.state.LockRuns(true)
	fmt.Fprintf(w, "{\"Locked\": %t}\n", e.state.ReadRunLock())
}

func (e *HTTPEngine) removeChefLock(w http.ResponseWriter, r *http.Request) {
	setContentJSON(w)
	e.state.LockRuns(false)
	fmt.Fprintf(w, "{\"Locked\": %t}\n", e.state.ReadRunLock())
}
