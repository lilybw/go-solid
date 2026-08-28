package shim_f

/*

// Handler for redirecting requests to the forwarding port
func RedirectToTLS(config *config.AppConfig, state *services.AppState, w http.ResponseWriter, r *http.Request) {
	logger := state.Log
	parts := strings.Split(r.Host, ":")
	hostname := parts[0]
	target := "https://" + hostname + ":" + config.ServerPort + r.RequestURI
	logger.WithFields(log.Fields{
		"package":  "main",
		"function": "redirect",
		"uri":      r.Host,
		"from":     r.URL,
		"to":       target,
	}).Info("Forwarding to TLS")
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func CreateTLSConfiguration(config *config.AppConfig, state *services.AppState) (*tls.Config, string, string, error) {
	logger := state.Log

	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// No CipherSuites: Go picks secure defaults for TLS 1.2 and ignores
		// the field entirely for TLS 1.3. A hand-maintained list only rots.
		// No CurvePreferences: defaults now include X25519 (and X25519MLKEM768
		// in Go 1.24+), which are faster and safer than P-521.
		// No PreferServerCipherSuites: deprecated and a no-op since Go 1.17.
		NextProtos: []string{"h2", "http/1.1"},
	}

	tlsCert := config.TLSCertPath
	tlsKey := config.TLSKeyPath

	logger.WithFields(log.Fields{
		"package":  "tls",
		"function": "CreateTLSConfiguration",
		"tlsCert":  tlsCert,
		"tlsKey":   tlsKey,
	}).Info("TLS certificate and key location")

	// Both or neither. A half-configured pair is the silent-failure case.
	if (tlsCert == "") != (tlsKey == "") {
		return nil, "", "", fmt.Errorf(
			"TLS misconfigured: cert path %q and key path %q must both be set or both be empty",
			tlsCert, tlsKey)
	}

	if tlsCert == "" {
		logger.WithFields(log.Fields{
			"package":  "tls",
			"function": "CreateTLSConfiguration",
		}).Warn("No TLS certificate configured — server will serve plaintext HTTP")
		return cfg, "", "", nil
	}

	//  This catches unreadable files, mismatched cert/key, and malformed PEM at startup
	cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to load TLS keypair (%s, %s): %w", tlsCert, tlsKey, err)
	}
	cfg.Certificates = []tls.Certificate{cert}

	if leaf, parseErr := x509.ParseCertificate(cert.Certificate[0]); parseErr == nil {
		logger.WithFields(log.Fields{
			"package":  "tls",
			"function": "CreateTLSConfiguration",
			"subject":  leaf.Subject.CommonName,
			"dnsNames": leaf.DNSNames,
			"notAfter": leaf.NotAfter,
		}).Info("Loaded TLS certificate")
		if time.Now().After(leaf.NotAfter) {
			logger.Warn("TLS certificate is expired")
		}
	}

	return cfg, tlsCert, tlsKey, nil
}

*/

/*
import	"github.com/gorilla/mux"

func main() {
	config, configError := config.LoadConfg()
	if configError != nil {
		logrus.Fatalf("Failed to load configuration: %v", configError)
	}
	router := mux.NewRouter()
state, stateError := services.InitAppState(config, router)
	if stateError != nil {
		logrus.Fatalf("Failed to initialize application state: %v", stateError)
	}
	logger := state.Log
	logger.Info(config.ToString())

	var listener net.Listener
	var server http.Server

	_, apiErr := api.Apply(config, state, router)
	if apiErr != nil {
		logrus.Fatalf("Failed to apply API routes: %v", apiErr)
	}

	// Explicit serve on TCP via IPv4 and TCP via IPv6, unless config.Address
	// is non empty.
	listener, listenErr := net.Listen("tcp", config.ServerIP+":"+config.ServerPort)
	if listenErr != nil {
		logrus.Fatalf("Failed to listen on %s:%s: %v", config.ServerIP, config.ServerPort, listenErr)
	}

	// Prepare for TLS
	cfg, tlsCert, tlsKey, tlsError := internalTLS.CreateTLSConfiguration(config, state)
	if tlsError != nil {
		logrus.Fatalf("Failed to create TLS configuration: %v", tlsError)
	}

	server = http.Server{
		Handler:           router,
		TLSConfig:         cfg,
		ErrorLog:          log.New(logger.Writer(), "[server]", 0),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Redirection code
	if config.Forward != "" {
		logger.WithFields(logrus.Fields{
			"package":  "main",
			"function": "forwarding",
			"port":     config.Forward,
		}).Info("Starting forwarding server")
		go func() {
			if err := http.ListenAndServe(":"+config.Forward, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				internalTLS.RedirectToTLS(config, state, w, r)
			})); err != nil {
				logrus.Fatalf("Forwarding error: %v", err)
			}
		}()
	}

	// Create an error channel to catch failing servers.
	ec := make(chan error, 1)
	go func(ls ...net.Listener) {
		for _, l := range ls {
			go func() {
				withTLS := tlsCert != "" && tlsKey != ""
				logger.WithFields(logrus.Fields{
					"package":  "main",
					"function": "main",
					"address":  l.Addr(),
					"network":  l.Addr().Network(),
					"TLS":      withTLS,
				}).Info("Starting server on listener.")
				var e error
				if withTLS {
					e = server.ServeTLS(l, tlsCert, tlsKey)
				} else {
					e = server.Serve(l)
				}

				if e != nil {
					ec <- e
				}
			}()
		}
	}(listener)

	// If all servers fails, kill the application.
	for e := range ec {
		configError = e
		logger.WithFields(logrus.Fields{
			"package":  "main",
			"function": "main",
			"error":    configError,
		}).Fatal("Server died.")
	}
*/

/*
func InitAppState(cfg *config.AppConfig, router *mux.Router) (*AppState, error) {

	userService, err := integrations.InitUserService(cfg)
	if err != nil {
		return nil, err
	}

	var hmrCfg *hmr.HMRConfig
	if cfg.Mode == config.ModeDevelopment {
		hmrCfg = &hmr.HMRConfig{
			Mux: compat.MuxLikeFromRouterLike(router),
		}
	}

	staticCfg := &static.StaticConfig{
		Reactive: cfg.Mode == config.ModeDevelopment,
		Mux:      compat.MuxLikeFromRouterLike(router),
		Location: filepath.Join(u.WDOrPanic(), "static"),
		Ignore:   []static.FileSelectorPattern{"*.js", "*.md"},
	}

	bundler, err := go_solid.New(&go_solid.Config{
		Components: filepath.Join(u.WDOrPanic(), "frontend", "components"),
		HMR:        hmrCfg,
		Defaults: &go_solid.BehaviouralDefaults{
			HeadSegment: func(builder networking.HTMLHeadSegmentBuilder) {
				builder.SetTitle("HOTS")
			},
		},
		Static:           staticCfg,
		ReactiveRegistry: cfg.Mode == config.ModeDevelopment,
		LogLevel:         logging.LEVEL_TRACE,
	})

	if err != nil {
		return nil, fmt.Errorf("Failed to initialize templating engine: %v", err)
	}

	components := bundler.Registry().Map(func(k string, _ *registry.Component) string { return k })
	if len(components) == 0 {
		return nil, fmt.Errorf("No components found in the registry. Please ensure that the components directory is correctly set and contains valid components.")
	} else if cfg.Mode == config.ModeDevelopment {
		log.Infof("Components: %v", components)
	}

	appState := &AppState{
		CookieStore:     sessions.NewCookieStore([]byte("TheSimplestKeyToEverChoose")),
		SessionName:     "name",
		HotsSessionName: "hots_session",
		UserService:     userService,
		Log:             log.StandardLogger(),
		Templating:      bundler,
	}
	return appState, nil
}
*/

/* LOGS

time=20260828-092843.413 level=info msg="Changed log level." package=config function=LoadConfg fields.level=info
time=20260828-092843.425 level=info msg="Configuration directory." absolute-directory="E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0" package=main function=main configuration-directory="E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0" upload-directory="\\tmp\\plupload"
2026/08/28 09:28:43 [bundler.go#configValidationAndNormalization] user config:
{
    "Components": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\frontend\\components",
    "Workspace": "",
    "LogLevel": 1,
    "DisableCaching": false,
    "Generation": null,
    "ReactiveRegistry": true,
    "Rasterization": null,
    "Types": null,
    "Static": {
       "Location": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\static",
       "Reactive": true,
       "Ignore": [
          "*.js",
          "*.md"
       ],
       "MountPath": "",
       "Disabled": false,
       "InlineLimit": 0
    },
    "HMR": {
       "Disabled": false,
       "Path": ""
    },
    "Defaults": {}
 }
2026/08/28 09:28:43 normalized configuration:
{
    "Components": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\frontend\\components",
    "Workspace": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\frontend\\components\\.go_solid",
    "LogLevel": 1,
    "DisableCaching": false,
    "Generation": {
       "Alias": {
          "@go_solid/static": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\frontend\\components\\.go_solid\\modules\\static\\index.ts"
       },
       "Solid": {
          "Runtime": 0,
          "Development": false,
          "ModuleName": "solid-js/web",
          "HelperPrefix": "_$",
          "DisableEventDelegation": false,
          "RuntimeOverride": {}
       },
       "Sourcemap": 0,
       "Minify": true,
       "Dependencies": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\frontend\\components",
       "Disabled": false
    },
    "ReactiveRegistry": true,
    "Rasterization": {
       "Location": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\frontend\\components\\.go_solid",
       "ExpectCompleted": false,
       "Disabled": false
    },
    "Types": {
       "Check": "RUNTIME_AND_BOOT"
    },
    "Static": {
       "Location": "E:\\GitHub\\hti\\hots\\hermestraffic_hots\\hermestraffic.com\\v1.0\\static",
       "Reactive": true,
       "Ignore": [
          "*.js",
          "*.md"
       ],
       "MountPath": "/__go_solid_static__/",
       "Disabled": false,
       "InlineLimit": 262144
    },
    "HMR": {
       "Disabled": false,
       "Path": ""
    },
    "Defaults": {}
 }
2026/08/28 09:28:43 [go_solid] generated modules import as "@go_solid"/<name>. For editor resolution add "extends": ["E:/GitHub/hti/hots/hermestraffic_hots/hermestraffic.com/v1.0/frontend/components/.go_solid/tsconfig.paths.json"] to your tsconfig.json
time=20260828-092843.781 level=info msg="Components: [Home JobList Login NavBar TopBar]"
time=20260828-092843.781 level=info msg="AppConfig: {\n\tMode: development, \n\tTarget: local, \n\tAddress: localhost, \n\tPort: 9090, \n\tForward: , \n\tGeoServer: http://atlas.hermestraffic.com:8080/geoserver, \n\tVibrations: ../data/vibrations/, \n\tClips: ../data/clips/, \n\tVersion: HOTS 2.0, \n\tLogPath: ,\n\tLogLevel: info, \n\tTLSCert: ****, \n\tTLSKey: ****, \n\tSite: eu, \n\tSites: map[eu:{Url:https://hots.hermestraffic.com/ Name:European Union Flag:eu Description:European site Branding:hti} us:{Url:https://hots.us.hermestraffic.com/ Name:United States Flag:us Description:North American site Branding:hti}],\n\tUploadDir: /tmp}"
time=20260828-092843.788 level=info msg="TLS certificate and key location" function=CreateTLSConfiguration tlsCert=../certificates/hots.hermestraffic.com/certificate.crt tlsKey=../certificates/hots.hermestraffic.com/private.key package=tls
time=20260828-092843.799 level=info msg="Loaded TLS certificate" function=CreateTLSConfiguration subject=hots.hermestraffic.com dnsNames="[hots.hermestraffic.com]" notAfter="2023-06-02 23:59:59 +0000 UTC" package=tls
time=20260828-092843.799 level=warning msg="TLS certificate is expired"
time=20260828-092843.799 level=info msg="Starting server on listener." package=main function=main address="127.0.0.1:9090" network=tcp TLS=true
*/
