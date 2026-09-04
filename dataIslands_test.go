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

// Every middleware registered runs, not the last one. Both existing cases
// register exactly one, which is how a With that assigned rather than appended
// survived: a consumer chaining two got the second and silently lost the first.
func Test_dataIslandMiddlewareChainedRegistrations(t *testing.T) {
	island := func(id string) networking_shr.Middleware {
		return func(artifact networking_shr.LimitedAccessView, _ *networking_shr.RequestBehaviour) error {
			artifact.PutDataIsland(id, `{"id":"`+id+`"}`)
			return nil
		}
	}

	bundler, err := NewEphemeral(&EphemeralConfig{
		Defaults: &BehaviouralDefaults{
			Requests: func(req *networking_shr.RequestBehaviourBuilder) {
				req.With(island("default-first")).With(island("default-second"))
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := bundler.Anonymous(`(props)=><div>ThisElementIsJustForShow</div>`, map[string]string{"default": "props"}).
		AlterHTTPBehaviour(func(req *networking_shr.RequestBehaviourBuilder) {
			req.With(island("per-call"))
		}).Render()
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"default-first", "default-second", "per-call"} {
		if !strings.Contains(rendered.HTML, id) {
			t.Errorf("data island %q is absent; its middleware was registered and never ran:\n%s", id, rendered.HTML)
		}
	}
}

// A data island belongs to the render that produced it. The artifact behind it
// is a cache entry shared by every request for that component, so an island
// written onto the artifact is one user's data on the next user's page.
func Test_dataIslandsDoNotOutliveTheirRender(t *testing.T) {
	bundler, err := NewEphemeral(&EphemeralConfig{})
	if err != nil {
		t.Fatal(err)
	}

	const source = `(props)=><div>ThisElementIsJustForShow</div>`
	props := map[string]string{"default": "props"}

	if _, err := bundler.Anonymous(source, props).
		AlterHTTPBehaviour(func(req *networking_shr.RequestBehaviourBuilder) {
			req.With(func(artifact networking_shr.LimitedAccessView, _ *networking_shr.RequestBehaviour) error {
				artifact.PutDataIsland("session-of-the-first-caller", `{"user":"alice"}`)
				return nil
			})
		}).Render(); err != nil {
		t.Fatal(err)
	}

	// Same component, same props: a cache hit, and no middleware this time.
	second, err := bundler.Anonymous(source, props).Render()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second.HTML, "session-of-the-first-caller") {
		t.Fatalf("the previous render's data island was served to a caller that never asked for it:\n%s", second.HTML)
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
