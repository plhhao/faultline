package config_test

import (
	"errors"
	"faultline/internal/config"
	"strings"
	"testing"
)

func TestPathPatternValidationAndRoundTrip(t *testing.T) {
	for _, pattern := range []string{"/", "/payment/:id", "/payment/:id/items/:item_id", "/literal/", "/:a_1", "/a//b"} {
		t.Run(pattern, func(t *testing.T) {
			d := parse(t, strings.Replace(validYAML, "path: /payments", "path_pattern: "+pattern, 1))
			data, err := config.Encode(d.Config())
			if err != nil {
				t.Fatal(err)
			}
			next := parse(t, string(data))
			if !d.Equal(next) || next.Config().Proxies[0].Rules[0].Match.PathPattern != pattern {
				t.Fatal("round trip")
			}
			old := parse(t, validYAML)
			if old.Equal(d) || !old.RestartCompatible(d) {
				t.Fatal("change classification")
			}
		})
	}
	for _, value := range []string{"payment/:id", "/payment/:", "/:1id", "/:a-b", "/:id/:id", "/item-:id", "/:id.json", "/a/*", "/a/**", "/a?x=1", "/a#fragment", "/:a:b"} {
		_, err := config.Parse([]byte(strings.Replace(validYAML, "path: /payments", "path_pattern: '"+value+"'", 1)), "test.yaml")
		var field *config.FieldError
		if !errors.As(err, &field) || field.Path != "proxies[0].rules[0].match.path_pattern" {
			t.Fatalf("%q: %v", value, err)
		}
	}
	_, err := config.Parse([]byte(strings.Replace(validYAML, "path: /payments", "path: /payments\n          path_pattern: /:id", 1)), "test.yaml")
	if err == nil {
		t.Fatal("combined matchers accepted")
	}
}
