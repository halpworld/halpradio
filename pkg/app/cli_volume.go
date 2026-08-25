package app

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/util"
)

// RunVolume handles `halpradio volume [value] [--json]`.
func RunVolume(args []string, out io.Writer) (bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintVolumeHelp(out)
		return true, nil
	}

	isJSON := false
	var targetVal string
	for _, arg := range args {
		if arg == "--json" || arg == "-json" || arg == "-j" {
			isJSON = true
		} else if targetVal == "" && !strings.HasPrefix(arg, "-") {
			targetVal = arg
		} else if targetVal == "" && (strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-")) {
			targetVal = arg
		}
	}

	// 1. Check if an active instance is running via IPC
	statusResp, err := desktop.SendIPCCommand("", "status")
	isLive := (err == nil && statusResp != nil && statusResp.Success)

	if targetVal == "" {
		// Query volume
		if isLive && statusResp.Status != nil {
			vol := statusResp.Status.Volume
			if isJSON {
				data, _ := json.MarshalIndent(map[string]interface{}{
					"volume": vol,
					"muted":  statusResp.Status.Muted,
					"status": statusResp.Status.Status,
				}, "", "  ")
				fmt.Fprintln(out, string(data))
				return true, nil
			}
			if statusResp.Status.Muted {
				fmt.Fprintf(out, "Volume: %d%% [MUTED]\n", vol)
			} else {
				fmt.Fprintf(out, "Volume: %d%%\n", vol)
			}
			return true, nil
		}

		// Fallback to saved config volume if offline
		cfg, _ := util.LoadConfig()
		if isJSON {
			data, _ := json.MarshalIndent(map[string]interface{}{
				"volume": cfg.Volume,
				"status": "offline",
			}, "", "  ")
			fmt.Fprintln(out, string(data))
			return true, nil
		}
		fmt.Fprintf(out, "Volume: %d%% (saved default, halpradio offline)\n", cfg.Volume)
		return true, nil
	}

	// Handle volume modification
	if strings.EqualFold(targetVal, "mute") || strings.EqualFold(targetVal, "unmute") || strings.EqualFold(targetVal, "toggle") {
		if isLive {
			resp, err := desktop.SendIPCCommand("", "mute")
			if err != nil {
				return false, err
			}
			if isJSON {
				data, _ := json.MarshalIndent(resp, "", "  ")
				fmt.Fprintln(out, string(data))
				return true, nil
			}
			fmt.Fprintln(out, "✓ Toggled mute.")
			return true, nil
		}
		fmt.Fprintln(out, "Error: halpradio is not currently running.")
		return false, fmt.Errorf("halpradio not running")
	}

	// Relative volume stepping: e.g. +5, -10, +1, -1
	if strings.HasPrefix(targetVal, "+") || strings.HasPrefix(targetVal, "-") {
		step, err := strconv.Atoi(targetVal)
		if err != nil {
			fmt.Fprintf(out, "Invalid relative volume %q\n", targetVal)
			return false, err
		}

		if isLive {
			// Send volup/voldown commands in 5% increments
			count := step / 5
			if count == 0 {
				if step > 0 {
					count = 1
				} else {
					count = -1
				}
			}

			action := "volup"
			if count < 0 {
				action = "voldown"
				count = -count
			}

			var lastResp *desktop.IPCResponse
			for i := 0; i < count; i++ {
				lastResp, _ = desktop.SendIPCCommand("", action)
			}

			if isJSON {
				if lastResp != nil && lastResp.Status != nil {
					data, _ := json.MarshalIndent(lastResp.Status, "", "  ")
					fmt.Fprintln(out, string(data))
				} else {
					fmt.Fprintln(out, `{"status":"ok"}`)
				}
				return true, nil
			}

			if lastResp != nil && lastResp.Status != nil {
				fmt.Fprintf(out, "✓ Volume: %d%%\n", lastResp.Status.Volume)
			} else {
				fmt.Fprintf(out, "✓ Volume adjusted (%s)\n", targetVal)
			}
			return true, nil
		}

		// Update offline config
		cfg, _ := util.LoadConfig()
		cfg.Volume += step
		if cfg.Volume < 0 {
			cfg.Volume = 0
		}
		if cfg.Volume > 100 {
			cfg.Volume = 100
		}
		_ = util.SaveConfig(cfg)
		if isJSON {
			data, _ := json.MarshalIndent(map[string]interface{}{"volume": cfg.Volume, "status": "saved"}, "", "  ")
			fmt.Fprintln(out, string(data))
			return true, nil
		}
		fmt.Fprintf(out, "✓ Default volume set to %d%%\n", cfg.Volume)
		return true, nil
	}

	// Absolute volume value: e.g. 50, 80
	val, err := strconv.Atoi(targetVal)
	if err != nil || val < 0 || val > 100 {
		fmt.Fprintf(out, "Error: volume must be an integer between 0 and 100 (got %q)\n", targetVal)
		return false, fmt.Errorf("invalid volume value: %s", targetVal)
	}

	if isLive {
		currentVol := 80
		if statusResp.Status != nil {
			currentVol = statusResp.Status.Volume
		}
		diff := val - currentVol
		count := diff / 5
		action := "volup"
		if count < 0 {
			action = "voldown"
			count = -count
		}
		var lastResp *desktop.IPCResponse
		for i := 0; i < count; i++ {
			lastResp, _ = desktop.SendIPCCommand("", action)
		}

		if isJSON {
			if lastResp != nil && lastResp.Status != nil {
				data, _ := json.MarshalIndent(lastResp.Status, "", "  ")
				fmt.Fprintln(out, string(data))
			} else {
				fmt.Fprintln(out, `{"status":"ok"}`)
			}
			return true, nil
		}

		if lastResp != nil && lastResp.Status != nil {
			fmt.Fprintf(out, "✓ Volume: %d%%\n", lastResp.Status.Volume)
		} else {
			fmt.Fprintf(out, "✓ Volume set to %d%%\n", val)
		}
		return true, nil
	}

	// Update offline config
	cfg, _ := util.LoadConfig()
	cfg.Volume = val
	_ = util.SaveConfig(cfg)
	if isJSON {
		data, _ := json.MarshalIndent(map[string]interface{}{"volume": cfg.Volume, "status": "saved"}, "", "  ")
		fmt.Fprintln(out, string(data))
		return true, nil
	}
	fmt.Fprintf(out, "✓ Default volume set to %d%%\n", cfg.Volume)
	return true, nil
}
