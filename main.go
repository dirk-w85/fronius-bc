package main

import (
	"encoding/json"
	"os"
	"log/slog"
	"fmt"
	"net/http"
	"time"
	"github.com/spf13/viper"
)

//---------------- STRUCTS -----------------
type LOWESTPrice struct {
	Price 	float64
	Iter 	int
	Start	time.Time
	End 	time.Time
	SoC		float64
	BatteryMode string
	BatteryLimit float64
	CurrentPrice float64
}

type EVCCState struct {
	Result struct {		
		BatteryMode				string `json:"batteryMode"`
		BatterySoc              float64 `json:"batterySoc"`
		TariffGrid				float64 `json:"tariffGrid"`
		Forecast                struct {
			Grid []struct {
				Start time.Time `json:"start"`
				End   time.Time `json:"end"`
				Price float64   `json:"value"`
			} `json:"grid"`
		} `json:"forecast"`
	}
}

//---------------- FUNCTIONS -----------------
func getEVCCState(url string, start int, end int, batteryLimit float64)(LOWESTPrice){
	slog.Info(fmt.Sprintf("Checking for lowest Price between %d:00 and %d:00", start, end))
	evccURL := fmt.Sprintf("http://%s/api/state", url)

	res, err := http.Get(evccURL)
    if err != nil {
        slog.Error("error making http request","ERR", err)
        os.Exit(1)
    }
    defer res.Body.Close()

	var evccResp EVCCState
	err = json.NewDecoder(res.Body).Decode(&evccResp)
	if err != nil {
		slog.Error("error decoding data", "ERR", err)
	}

	var lowestPrice LOWESTPrice
	if len(evccResp.Result.Forecast.Grid) >0 {
		lowestPrice.Price = evccResp.Result.Forecast.Grid[0].Price
		lowestPrice.CurrentPrice = evccResp.Result.TariffGrid

		// Lowest Price between START and END
		for i :=start; i<=end; i++ {
			if evccResp.Result.Forecast.Grid[i].Price <= lowestPrice.Price {
				lowestPrice.Price = evccResp.Result.Forecast.Grid[i].Price
				//lowestPrice.Iter = i
				lowestPrice.Start = evccResp.Result.Forecast.Grid[i].Start
				lowestPrice.End = evccResp.Result.Forecast.Grid[i].End
				lowestPrice.BatteryMode = evccResp.Result.BatteryMode
			}
		}
	}else{
		lowestPrice.Price = 0
		lowestPrice.CurrentPrice = 0
	}

	lowestPrice.BatteryLimit = batteryLimit
	lowestPrice.SoC = evccResp.Result.BatterySoc
	
	slog.Debug(fmt.Sprintf("Battery SoC: %.0f - (Limit: %.0f)", lowestPrice.SoC, lowestPrice.BatteryLimit))
	slog.Debug(fmt.Sprintf("Battery Mode: %s", lowestPrice.BatteryMode))
	slog.Info(fmt.Sprintf("Current Price: %.3f Euro/kWh", lowestPrice.CurrentPrice))

	return lowestPrice	
}

func setEVCCCharging(url string, evccState LOWESTPrice){	
	startCharging := false
	timeCheckPassed := false
	evccURL := fmt.Sprintf("http://%s/api/batterygridchargelimit/%f", url, 0.0)

	// Adding a bit of margin for Energy Price
	//evccState.Price = evccState.Price + 0.0001

	slog.Info(fmt.Sprintf("Lowest Price: %.3f Euro/kWh starting at %s", evccState.Price, evccState.Start))
	slog.Debug(fmt.Sprintf("Lowest Price: %.3f Euro/kWh ending at %s", evccState.Price, evccState.End)) 

	t := time.Now()

	if t.Hour() >= evccState.Start.Hour() && t.Hour() < evccState.End.Hour(){
		// Morning Check
		slog.Debug("We are in the lowest Price Window!")
		timeCheckPassed = true
	}else{
		slog.Debug("We are NOT in the lowest Price Window!")
		timeCheckPassed = false
	}	

	// When Battery is currently NOT charging (normal or charge)
	if evccState.BatteryMode == "normal" || evccState.BatteryMode == "unknown" {
		// When Battery is not at SoC Limit and the time check was passed
		if evccState.SoC < evccState.BatteryLimit && timeCheckPassed == true {
			// Set start Charging
			startCharging = true
		}else{
			slog.Info(fmt.Sprintf("Not Charging! Battery above configured Limit or Time Check not passed."))
			startCharging = false
		}
	}else if evccState.BatteryMode == "charge"{
		slog.Warn(fmt.Sprintf("Battery already charging! Battery SoC at %.0f - (Limit: %.0f)", evccState.SoC, evccState.BatteryLimit))
		if evccState.CurrentPrice > evccState.Price{
			slog.Debug(fmt.Sprintf("Current Price %f higher then lowest detected Price %f. Setting Price to 0.0 to stopp charging...", evccState.CurrentPrice, evccState.Price))
			evccState.Price = 0.000
			startCharging = false
		}

		if evccState.SoC > evccState.BatteryLimit {
			slog.Debug(fmt.Sprintf("Battery SoC is above limit of %.0f. Setting Price to 0.0 to stopp charging...\n", evccState.BatteryLimit))
			evccState.Price = 0.000
			startCharging = false
		}
	}

	evccURL = fmt.Sprintf("http://%s/api/batterygridchargelimit/%f", url, evccState.Price)

	if evccState.BatteryMode == "normal" && startCharging  || evccState.BatteryMode == "unknown" && startCharging {
		slog.Debug("Informing EVCC about the change/update to START charging via HTTP POST...")
		slog.Debug(evccURL)
		res, err := http.Post(evccURL, "application/json", nil)
		if err != nil {
			slog.Error("error making http request","ERR", err)
			os.Exit(1)
		}
		defer res.Body.Close()
		slog.Info(fmt.Sprintf("Charging started!"))
	} else if evccState.BatteryMode == "charge" && evccState.SoC <= evccState.BatteryLimit {
		slog.Info("Charging! Battery Limit not yet reached.")
	}else if evccState.BatteryMode == "charge" && startCharging == false  {
		slog.Debug("Informing EVCC about the change/update to STOP charging via HTTP POST...")
		slog.Debug(evccURL)
		res, err := http.Post(evccURL, "application/json", nil)
		if err != nil {
			slog.Error("error making http request","ERR", err)
			os.Exit(1)
		}
		defer res.Body.Close()
		slog.Info(fmt.Sprintf("Charging stopped!"))
	}
}

//---------------- MAIN -----------------
func main() {	
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("toml") // REQUIRED if the config file does not have the extension in the name
	viper.AddConfigPath("/etc/fronius-bc/")   // path to look for the config file in
	viper.AddConfigPath(".")               // optionally look for config in the working directory
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil { // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %w \n", err))
	}

	var evccHost = viper.GetString("evcc.host")
	//var evccBatteryLimit = viper.GetFloat64("evcc.batteryLimit")
	var Interval = viper.GetInt("global.interval")
	var evccState LOWESTPrice
	var version = "undefined"

	fmt.Println("\n-- Fronius Battery Control via EVCC --\n")

	logger := slog.New(slog.NewJSONHandler(os.Stderr,nil))
	if viper.GetBool("global.debug") {
		logger = slog.New(slog.NewJSONHandler(os.Stderr,&slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	
	slog.SetDefault(logger)
	slog.Debug("Application started")
	slog.Debug("Config Setting","Version",version)

	if Interval != 0 {
		for {
			// Refresh Config from File
			err = viper.ReadInConfig()
			if err != nil { // Handle errors reading the config file
				panic(fmt.Errorf("Fatal error config file: %w \n", err))
			}
			Interval = viper.GetInt("global.interval")
			//evccBatteryLimit = viper.GetFloat64("evcc.batteryLimit")
			evccMorningStart := viper.GetInt("evcc.morning.start")
			evccMorningEnd := viper.GetInt("evcc.morning.end")
			evccMorningLimit := viper.GetFloat64("evcc.morning.batteryLimit")
			evccAfternoonStart := viper.GetInt("evcc.afternoon.start")
			evccAfternoonEnd := viper.GetInt("evcc.afternoon.end")
			evccAfternoonLimit := viper.GetFloat64("evcc.afternoon.batteryLimit")

			slog.Debug(fmt.Sprintf("Current Time: %s", time.Now()))
			slog.Debug("Config Setting","Interval", Interval)
			slog.Debug("Config Setting","EVCC_Host",evccHost)
			slog.Debug("Config Setting","Morning_Battery_Limit",evccMorningLimit)
			slog.Debug("Config Setting","Morning_Start",evccMorningStart)
			slog.Debug("Config Setting","Morning_End",evccMorningEnd)
			slog.Debug("Config Setting","Afternoon_Start",evccAfternoonStart)
			slog.Debug("Config Setting","Afternoon_End",evccAfternoonEnd)
			slog.Debug("Config Setting","Afternoon_Battery_Limit",evccAfternoonLimit)
			
			t := time.Now() 

			switch t.Hour(); {
				case t.Hour() >= evccMorningStart && t.Hour() < evccMorningEnd:
					// Morning Check
					slog.Debug("Performing Morning Check based on current time")
					evccState = getEVCCState(evccHost, evccMorningStart, evccMorningEnd, evccMorningLimit)	
					setEVCCCharging(evccHost,evccState)
				case t.Hour() >= evccAfternoonStart && t.Hour() < evccAfternoonEnd:
					// Afternnon Check
					slog.Debug("Performing Afternoon Check based on current time")
					evccState = getEVCCState(evccHost, evccAfternoonStart, evccAfternoonEnd,evccAfternoonLimit)
					setEVCCCharging(evccHost,evccState)
				default:
					slog.Info("OFF SHIFT! Not in charge right now.")
			}
			
			// Sleep for X Seconds
			slog.Debug(fmt.Sprintf("Sleeping for %d seconds", Interval))
			time.Sleep(time.Duration(Interval) * time.Second)
			fmt.Println("")
		}
	}
}
