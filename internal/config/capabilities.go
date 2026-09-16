package config

// Actions lists editor choices; Parse remains authoritative for field combinations.
func Actions(protocol string) []string {
	actions := []string{"delay", "hold_request", "hold_response", "truncate", "throttle"}
	if protocol == "http1" || protocol == "http2" {
		actions = append(actions, "respond")
	}
	if protocol == "http1" {
		actions = append(actions, "close_connection")
	}
	return actions
}
