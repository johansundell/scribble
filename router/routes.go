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
		// CheckClient refuses clients too old for the API (ADR 8).
		Route{Name: "ListNotes", Method: "GET", Pattern: "/notes", HandlerFunc: handler.ListNotes, UseAuth: true, CheckClient: true},
		Route{Name: "CreateNote", Method: "POST", Pattern: "/notes", HandlerFunc: handler.CreateNote, UseAuth: true, CheckClient: true},
		Route{Name: "GetNote", Method: "GET", Pattern: "/notes/:id", HandlerFunc: handler.GetNote, UseAuth: true, CheckClient: true},
		Route{Name: "UpdateNote", Method: "PUT", Pattern: "/notes/:id", HandlerFunc: handler.UpdateNote, UseAuth: true, CheckClient: true},
		Route{Name: "DeleteNote", Method: "DELETE", Pattern: "/notes/:id", HandlerFunc: handler.DeleteNote, UseAuth: true, CheckClient: true},
		Route{Name: "NoteBacklinks", Method: "GET", Pattern: "/notes/:id/backlinks", HandlerFunc: handler.NoteBacklinks, UseAuth: true, CheckClient: true},
		Route{Name: "RenameNote", Method: "POST", Pattern: "/notes/:id/rename", HandlerFunc: handler.RenameNote, UseAuth: true, CheckClient: true},
		Route{Name: "NoteChanges", Method: "GET", Pattern: "/changes", HandlerFunc: handler.NoteChanges, UseAuth: true, CheckClient: true},
		Route{Name: "ListProjects", Method: "GET", Pattern: "/projects", HandlerFunc: handler.ListProjects, UseAuth: true, CheckClient: true},
		// The web app (Web: registered only when WEB_PIN is set). It logs in
		// with the PIN, not AUTH_TOKEN, and is never request-logged: the log
		// would copy PINs and note bodies.
		Route{Name: "LoginPage", Method: "GET", Pattern: handlers.LoginPath, HandlerFunc: handler.LoginPage, Web: true},
		Route{Name: "Login", Method: "POST", Pattern: handlers.LoginPath, HandlerFunc: handler.Login, Web: true, AllowCrossOrigin: true},
		Route{Name: "Logout", Method: "POST", Pattern: "/app/logout", HandlerFunc: handler.Logout, Web: true, AllowCrossOrigin: true},
		// No session: phones fetch the manifest and icons before login (#65).
		Route{Name: "AppManifest", Method: "GET", Pattern: handlers.ManifestPath, HandlerFunc: handler.AppManifest, Web: true},
		Route{Name: "App", Method: "GET", Pattern: handlers.AppHome, HandlerFunc: handler.App, Web: true, UseSession: true},
		Route{Name: "AppNote", Method: "GET", Pattern: handlers.AppHome + "/notes/:id", HandlerFunc: handler.AppNote, Web: true, UseSession: true},
		Route{Name: "AppEdit", Method: "GET", Pattern: handlers.AppHome + "/notes/:id/edit", HandlerFunc: handler.AppEdit, Web: true, UseSession: true},
		Route{Name: "AppSave", Method: "POST", Pattern: handlers.AppHome + "/notes/:id/edit", HandlerFunc: handler.AppSave, Web: true, UseSession: true},
		Route{Name: "AppRenameForm", Method: "GET", Pattern: handlers.AppHome + "/notes/:id/rename", HandlerFunc: handler.AppRenameForm, Web: true, UseSession: true},
		Route{Name: "AppRename", Method: "POST", Pattern: handlers.AppHome + "/notes/:id/rename", HandlerFunc: handler.AppRename, Web: true, UseSession: true},
		Route{Name: "AppDeleteForm", Method: "GET", Pattern: handlers.AppHome + "/notes/:id/delete", HandlerFunc: handler.AppDeleteForm, Web: true, UseSession: true},
		Route{Name: "AppDelete", Method: "POST", Pattern: handlers.AppHome + "/notes/:id/delete", HandlerFunc: handler.AppDelete, Web: true, UseSession: true},
		Route{Name: "AppShareForm", Method: "GET", Pattern: handlers.AppHome + "/notes/:id/share", HandlerFunc: handler.AppShareForm, Web: true, UseSession: true},
		Route{Name: "AppShare", Method: "POST", Pattern: handlers.AppHome + "/notes/:id/share", HandlerFunc: handler.AppShare, Web: true, UseSession: true},
		// A shared note's public page (#83): no session, the token is the key.
		Route{Name: "Shared", Method: "GET", Pattern: handlers.SharedPath(":token"), HandlerFunc: handler.Shared, Web: true},
		Route{Name: "AppNew", Method: "GET", Pattern: handlers.NewPath, HandlerFunc: handler.AppNew, Web: true, UseSession: true},
		Route{Name: "AppPreview", Method: "POST", Pattern: handlers.PreviewPath, HandlerFunc: handler.AppPreview, Web: true, UseSession: true},
		Route{Name: "AppCreate", Method: "POST", Pattern: handlers.NewPath, HandlerFunc: handler.AppCreate, Web: true, UseSession: true},
		// *title, not :title: a title may hold a '/' (escaped as %2F).
		Route{Name: "AppTitle", Method: "GET", Pattern: handlers.AppHome + "/title/*title", HandlerFunc: handler.AppTitle, Web: true, UseSession: true},
	}
}
