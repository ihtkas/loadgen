package loadgen

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (lg *LoadGen) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Path
	switch url {
	case MetricsPathStr:
		promhttp.Handler().ServeHTTP(w, r)
	case WorkersPathStr:
		lg.handleWorkersReq(w, r)
	case RampUpDurationPathStr:
		lg.handleRampUpDurationReq(w, r)
	case RampUpFactorPathStr:
		lg.handleRampUpFactorReq(w, r)
	}
}

func (lg *LoadGen) handleWorkersReq(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	if val := r.FormValue("val"); val != "" {
		workers, err := strconv.Atoi(val)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lg.setWorkers(workers)
	} else {
		w.Write([]byte(fmt.Sprint(lg.getWorkersCount())))
	}
}

func (lg *LoadGen) handleRampUpDurationReq(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	if val := r.FormValue("val"); val != "" {
		dur, err := time.ParseDuration(val)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lg.setRampUpDuration(dur)
	} else {
		w.Write([]byte(fmt.Sprint(lg.getRampUpDuration())))
	}
}
func (lg *LoadGen) handleRampUpFactorReq(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	if val := r.FormValue("val"); val != "" {
		factor, err := strconv.ParseFloat(val, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lg.setRampUpFactor(factor)
	} else {
		w.Write([]byte(fmt.Sprint(lg.getRampUpFactor())))
	}
}
