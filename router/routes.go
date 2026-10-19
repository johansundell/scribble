package router

import (
	"github.com/johansundell/scribble/handlers"
)

// GetRoutes returns default Route configurations for the provided handler
func GetRoutes(handler *handlers.Handler) Routes {
	return Routes{
		Route{
			Name:        "HealthCheck",
			Method:      "GET",
			Pattern:     "/",
			HandlerFunc: handler.HealthCheck,
		},
		// The same check on the path deploy scripts and monitors expect.
		Route{
			Name:        "Healthz",
			Method:      "GET",
			Pattern:     "/healthz",
			HandlerFunc: handler.HealthCheck,
		},
		Route{
			Name:        "Ping",
			Method:      "GET",
			Pattern:     "/ping/:argument",
			HandlerFunc: handler.Ping,
			UseLogger:   true,
		},
		Route{
			Name:        "Pong",
			Method:      "POST",
			Pattern:     "/pong",
			HandlerFunc: handler.Pong,
			UseLogger:   true,
			UseAuth:     true,
		},
		Route{
			Name:        "GetLogs",
			Method:      "GET",
			Pattern:     "/logs/:from/:to",
			HandlerFunc: handler.GetLogs,
			UseAuth:     true,
		},
		// Notes. UseLogger is deliberately off: the request log stores full
		// request and response bodies, which would copy every note into it.
		Route{Name: "ListNotes", Method: "GET", Pattern: "/notes", HandlerFunc: handler.ListNotes, UseAuth: true},
		Route{Name: "CreateNote", Method: "POST", Pattern: "/notes", HandlerFunc: handler.CreateNote, UseAuth: true},
		Route{Name: "GetNote", Method: "GET", Pattern: "/notes/:id", HandlerFunc: handler.GetNote, UseAuth: true},
		Route{Name: "UpdateNote", Method: "PUT", Pattern: "/notes/:id", HandlerFunc: handler.UpdateNote, UseAuth: true},
		Route{Name: "DeleteNote", Method: "DELETE", Pattern: "/notes/:id", HandlerFunc: handler.DeleteNote, UseAuth: true},
		Route{Name: "NoteBacklinks", Method: "GET", Pattern: "/notes/:id/backlinks", HandlerFunc: handler.NoteBacklinks, UseAuth: true},
		Route{Name: "RenameNote", Method: "POST", Pattern: "/notes/:id/rename", HandlerFunc: handler.RenameNote, UseAuth: true},
		Route{Name: "NoteChanges", Method: "GET", Pattern: "/changes", HandlerFunc: handler.NoteChanges, UseAuth: true},
		Route{Name: "ListProjects", Method: "GET", Pattern: "/projects", HandlerFunc: handler.ListProjects, UseAuth: true},
		// The web app (Web: registered only when WEB_PIN is set). It logs in
		// with the PIN, not AUTH_TOKEN, and is never request-logged: the log
		// would copy PINs and note bodies.
		Route{Name: "LoginPage", Method: "GET", Pattern: handlers.LoginPath, HandlerFunc: handler.LoginPage, Web: true},
		Route{Name: "Login", Method: "POST", Pattern: handlers.LoginPath, HandlerFunc: handler.Login, Web: true, AllowCrossOrigin: true},
		Route{Name: "Logout", Method: "POST", Pattern: "/app/logout", HandlerFunc: handler.Logout, Web: true, AllowCrossOrigin: true},
		Route{Name: "App", Method: "GET", Pattern: handlers.AppHome, HandlerFunc: handler.App, Web: true, UseSession: true},
	}
}
