package shim_e

/*
type ResponseDataInterceptor struct {
	http.ResponseWriter
	Status int
}

func (rdi *ResponseDataInterceptor) WriteHeader(code int) {
	rdi.Status = code
	rdi.ResponseWriter.WriteHeader(code)
}
func (rdi *ResponseDataInterceptor) Write(b []byte) (int, error) {
	if rdi.Status == 0 {
		rdi.Status = http.StatusOK
	}
	return rdi.ResponseWriter.Write(b)
}

var withMiddleware = func(path string, f func(http.ResponseWriter, *http.Request)) *mux.Route {
	return rtr.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		interceptor := &ResponseDataInterceptor{ResponseWriter: w, Status: 999} // 999 is a placeholder for "not set"
		f(interceptor, r)
		LogRequests(ctx, interceptor, r)
	})
}

var NoSession = func(path string, handlers ...func(*ApiContext, http.ResponseWriter, *http.Request) *HttpResult) error {
	withMiddleware(path, func(w http.ResponseWriter, r *http.Request) {
		for _, handler := range handlers {
			if res := handler(ctx, w, r); res != nil && res.IsError() {
				http.Error(w, res.Message, res.Code)
				return
			}
		}
	})
	return nil
}

func main() {
	NoSession("/dev", func(ctx *ApiContext, w http.ResponseWriter, req *http.Request) *HttpResult {
		compName := req.URL.Query().Get("c")
		_, err := state.Templating.Prepare(compName, nil).ForRequest(w, req).Render()
		if err != nil {
			log.Println("Render failed for " + compName + " error:" + err.Error())

		}
		return AsHttpResult(200, 500, err)
	})
}
*/

/* SERVER LOGS
time=20260825-081528.042 level=info msg="[REQ LOG] 127.0.0.1:52675 --> GET    /dev                           --> 200   QUERY c=TopBar"
*/

/* BROWSER CONSOLE
dev?c=TopBar:11 Uncaught ReferenceError: return_tmpl$ is not defined
    at D (dev?c=TopBar:11:10664)
    at dev?c=TopBar:11:11033
    at dev?c=TopBar:11:8293
    at G.l (dev?c=TopBar:11:591)
    at y (dev?c=TopBar:11:4274)
    at G (dev?c=TopBar:11:632)
    at Q (dev?c=TopBar:11:8261)
    at dev?c=TopBar:11:11027
*/
