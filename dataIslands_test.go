package go_solid

import (
	"strings"
	"testing"

	networking_shr "github.com/lilybw/go-solid/shared/networking"
)

func Test_dataIslandMiddlewareAsDefaults(t *testing.T) {
	testJSONData := `{"key":"value","key1":[],"key2":{}}`
	testJSONId := "test-json-id"

	bundler, err := NewEphemeral(&EphemeralConfig{
		Defaults: &BehaviouralDefaults{
			Requests: func(req *networking_shr.RequestBehaviourBuilder) {
				req.With(func(artifact networking_shr.LimitedAccessView, _ *networking_shr.RequestBehaviour) error {
					artifact.PutDataIsland(testJSONId, testJSONData)
					return nil
				})
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.Anonymous(`(props)=><div>ThisElementIsJustForShow</div>`, map[string]string{"default": "props"}).Render()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(rendered.HTML, testJSONId) || !strings.Contains(rendered.HTML, testJSONData) {
		t.Fatalf("Expected custom data island to be included, it twasnt: %s", rendered.HTML)
	}
}

func Test_dataIslandMiddlewareUponRequest(t *testing.T) {
	testJSONData := `{"key":"value","key1":[],"key2":{}}`
	testJSONId := "test-json-id"

	bundler, err := NewEphemeral(&EphemeralConfig{})

	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.Anonymous(`(props)=><div>ThisElementIsJustForShow</div>`, map[string]string{"default": "props"}).
		AlterHTTPBehaviour(func(req *networking_shr.RequestBehaviourBuilder) {
			req.With(func(artifact networking_shr.LimitedAccessView, _ *networking_shr.RequestBehaviour) error {
				artifact.PutDataIsland(testJSONId, testJSONData)
				return nil
			})
		}).Render()

	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(rendered.HTML, testJSONId) || !strings.Contains(rendered.HTML, testJSONData) {
		t.Fatalf("Expected custom data island to be included, it twasnt: %s", rendered.HTML)
	}
}
