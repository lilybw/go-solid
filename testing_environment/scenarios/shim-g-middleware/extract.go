package shim_g

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	go_solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/shared/compat"
	"github.com/lilybw/go-solid/shared/hmr"
	"github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/registry"
	"github.com/lilybw/go-solid/shared/static"
)

type AppState struct {
	CookieStore        *sessions.CookieStore
	SessionName        string
	ServiceSessionName string
	UserService        *UserService
	Templating         *go_solid.Bundler
	Log                *log.Logger `json:"-"`
}

type AppConfig struct {
	Mode string `json:"mode"`
}

const ModeDevelopment = "development"

func InitAppState(cfg *AppConfig, router *mux.Router) (*AppState, error) {

	userService := &UserService{}

	hmrCfg := &hmr.HMRConfig{
		Mux: compat.MuxLikeFromRouterLike(router),
	}

	staticCfg := &static.StaticConfig{
		Reactive: cfg.Mode == ModeDevelopment,
		Mux:      compat.MuxLikeFromRouterLike(router),
		Location: filepath.Join(WDOrPanic(), "static"),
		Ignore:   []static.FileSelectorPattern{"*.js", "*.md"},
	}

	// TODO: This was directly copied from examples by the original author. This has security implications: session hijacking.
	// TODO: Generate the authetication key (go for 64 bytes) and choose the encryption key (go for 32 bytes). Do read:
	// TODO:     http://www.gorillatoolkit.org/pkg/sessions#NewCookieStore
	// TODO: tl;dr: Don't deploy with: "something-very-secret". It is shameful.
	cookies := sessions.NewCookieStore([]byte("TheSimplestKeyToEverChoose"))
	const userSessionName = "name"
	const serviceSessionName = "service_session"

	bundler, err := go_solid.New(&go_solid.Config{
		Components:       filepath.Join(WDOrPanic(), "frontend", "components"),
		HMR:              hmrCfg,
		Static:           staticCfg,
		ReactiveRegistry: cfg.Mode == ModeDevelopment,
		LogLevel:         logging.LEVEL_TRACE,
		Defaults: &go_solid.BehaviouralDefaults{
			HeadSegment: func(builder networking.HTMLHeadSegmentBuilder) {
				builder.SetTitle("Service")
			},
			Requests: func(builder *networking.RequestBehaviourBuilder) {
				builder.
					With(includeUserDataAsIsland(cookies, userService, serviceSessionName, userSessionName)).
					With(includeServiceStatistics)
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("Failed to initialize templating engine: %v", err)
	}

	components := bundler.Registry().Map(func(k string, _ *registry.Component) string { return k })
	if len(components) == 0 {
		return nil, fmt.Errorf("No components found in the registry. Please ensure that the components directory is correctly set and contains valid components.")
	}

	appState := &AppState{
		CookieStore:        cookies,
		SessionName:        userSessionName,
		ServiceSessionName: serviceSessionName,
		UserService:        userService,
		Templating:         bundler,
	}
	return appState, nil
}

type UserService struct{}

func (us *UserService) GetUserAccountInfoByName(name string) (*UserDataDTO, error) {
	return &UserDataDTO{Username: "shim g testing", Workspace: "shim g workspace"}, nil
}
func (us *UserService) Login(username string, password string) (*UserDataDTO, error) {
	user, err := us.GetUserAccountInfoByName(username)
	if err != nil {
		return nil, err
	}
	return user, err
}

type UserDataDTO struct {
	Username  string
	Workspace string
}

const (
	KEY_OF_LOCAL_USER_DATA         = "local-user-data"
	KEY_OF_HOTS_SERVICE_STATISTICS = "service-statistics"
)

var includeUserDataAsIsland = func(cookies *sessions.CookieStore, userService *UserService, serviceSessionName, userSessionName string) func(networking.LimitedAccessView, *networking.RequestBehaviour) error {
	log.Println("attaching middleware")
	return func(artifact networking.LimitedAccessView, req *networking.RequestBehaviour) error {
		log.Println("[services/appState#include user island] executing middleware")
		if req.R == nil {
			log.Println("[services/appState#include user island] Request was nil")
			return nil
		}
		session, err := cookies.Get(req.R, serviceSessionName)
		if err != nil {
			log.Println("[services/appState#include user island] Session failed")
			return err
		}
		username, ok := session.Values[userSessionName].(string)
		if !ok {
			log.Println("[services/appState#include user island] Username not available")
			return nil
		}
		userData, err := userService.GetUserAccountInfoByName(username)
		if err != nil {
			return fmt.Errorf("Error appending user data as data island, error getting user account info: %s", err.Error())
		}
		bytes, err := json.Marshal(userData)
		if err != nil {
			return fmt.Errorf("Error appending user data as data island, error marshalling json: %s", err.Error())
		}
		artifact.PutDataIsland(KEY_OF_LOCAL_USER_DATA, string(bytes))
		return nil
	}
}

func WDOrPanic() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("[config] Error getting working directory: %s", err.Error()))
	}
	return wd
}

type ServiceStaticsDTO struct {
	Version string `json:"version"`
}

var includeServiceStatistics = func(artifact networking.LimitedAccessView, req *networking.RequestBehaviour) error {
	bytes, err := json.Marshal(&ServiceStaticsDTO{Version: "2.0"})
	if err != nil {
		return err
	}

	artifact.PutDataIsland(KEY_OF_HOTS_SERVICE_STATISTICS, string(bytes))
	return nil
}
