package router

import (
	"github.com/johansundell/scribble/handlers"
)

// GetRoutes returns default Route configurations for the provided handler
func GetRoutes(handler *handlers.Handler) Routes {
	return Routes{
		// The web app is at the root; with it off, the root goes to the
		// health check (#92).
		Route{Name: "RootToHealth", Method: "GET", Pattern: "/", HandlerFunc: handler.RootToHealth, WebOff: true},
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
		Route{Name: "PinNote", Method: "POST", Pattern: "/notes/:id/pin", HandlerFunc: handler.PinNote, UseAuth: true, CheckClient: true},
		Route{Name: "RenameNote", Method: "POST", Pattern: "/notes/:id/rename", HandlerFunc: handler.RenameNote, UseAuth: true, CheckClient: true},
		// Share links (#142, ADR 11); POST answers 501 while the web app,
		// which serves the links, is off.
		Route{Name: "GetShare", Method: "GET", Pattern: "/notes/:id/share", HandlerFunc: handler.GetShare, UseAuth: true, CheckClient: true},
		Route{Name: "ShareNote", Method: "POST", Pattern: "/notes/:id/share", HandlerFunc: handler.ShareNote, UseAuth: true, CheckClient: true},
		Route{Name: "UnshareNote", Method: "DELETE", Pattern: "/notes/:id/share", HandlerFunc: handler.UnshareNote, UseAuth: true, CheckClient: true},
		Route{Name: "NoteChanges", Method: "GET", Pattern: "/changes", HandlerFunc: handler.NoteChanges, UseAuth: true, CheckClient: true},
		Route{Name: "ListProjects", Method: "GET", Pattern: "/projects", HandlerFunc: handler.ListProjects, UseAuth: true, CheckClient: true},
		// Note history (#55, ADR 7). gin matches the static /notes/deleted
		// before /notes/:id.
		Route{Name: "NoteVersions", Method: "GET", Pattern: "/notes/:id/versions", HandlerFunc: handler.NoteVersions, UseAuth: true, CheckClient: true},
		Route{Name: "NoteVersion", Method: "GET", Pattern: "/notes/:id/versions/:rev", HandlerFunc: handler.NoteVersion, UseAuth: true, CheckClient: true},
		Route{Name: "RestoreVersion", Method: "POST", Pattern: "/notes/:id/versions/:rev/restore", HandlerFunc: handler.RestoreVersion, UseAuth: true, CheckClient: true},
		Route{Name: "DeletedNotes", Method: "GET", Pattern: "/notes/deleted", HandlerFunc: handler.DeletedNotes, UseAuth: true, CheckClient: true},
		Route{Name: "UndeleteNote", Method: "POST", Pattern: "/notes/:id/undelete", HandlerFunc: handler.UndeleteNote, UseAuth: true, CheckClient: true},
		// Export and import as a ZIP of Markdown files (#117).
		Route{Name: "ExportNotes", Method: "GET", Pattern: "/export", HandlerFunc: handler.ExportNotes, UseAuth: true, CheckClient: true},
		Route{Name: "ImportNotes", Method: "POST", Pattern: "/import", HandlerFunc: handler.ImportNotes, UseAuth: true, CheckClient: true},
		// The web app (Web: registered only when WEB_PIN is set). It logs in
		// with the PIN, not AUTH_TOKEN, and is never request-logged: the log
		// would copy PINs and note bodies.
		Route{Name: "LoginPage", Method: "GET", Pattern: handlers.LoginPath, HandlerFunc: handler.LoginPage, Web: true},
		Route{Name: "Login", Method: "POST", Pattern: handlers.LoginPath, HandlerFunc: handler.Login, Web: true, AllowCrossOrigin: true},
		Route{Name: "Logout", Method: "POST", Pattern: handlers.LogoutPath, HandlerFunc: handler.Logout, Web: true, AllowCrossOrigin: true},
		// No session: phones fetch the manifest and icons before login (#65).
		Route{Name: "AppManifest", Method: "GET", Pattern: handlers.ManifestPath, HandlerFunc: handler.AppManifest, Web: true},
		Route{Name: "App", Method: "GET", Pattern: handlers.AppHome, HandlerFunc: handler.App, Web: true, UseSession: true},
		Route{Name: "AppNote", Method: "GET", Pattern: handlers.NotePrefix + ":id", HandlerFunc: handler.AppNote, Web: true, UseSession: true},
		Route{Name: "AppEdit", Method: "GET", Pattern: handlers.NotePrefix + ":id/edit", HandlerFunc: handler.AppEdit, Web: true, UseSession: true},
		Route{Name: "AppSave", Method: "POST", Pattern: handlers.NotePrefix + ":id/edit", HandlerFunc: handler.AppSave, Web: true, UseSession: true},
		Route{Name: "AppRenameForm", Method: "GET", Pattern: handlers.NotePrefix + ":id/rename", HandlerFunc: handler.AppRenameForm, Web: true, UseSession: true},
		Route{Name: "AppRename", Method: "POST", Pattern: handlers.NotePrefix + ":id/rename", HandlerFunc: handler.AppRename, Web: true, UseSession: true},
		Route{Name: "AppDeleteForm", Method: "GET", Pattern: handlers.NotePrefix + ":id/delete", HandlerFunc: handler.AppDeleteForm, Web: true, UseSession: true},
		Route{Name: "AppDelete", Method: "POST", Pattern: handlers.NotePrefix + ":id/delete", HandlerFunc: handler.AppDelete, Web: true, UseSession: true},
		Route{Name: "AppShareForm", Method: "GET", Pattern: handlers.NotePrefix + ":id/share", HandlerFunc: handler.AppShareForm, Web: true, UseSession: true},
		Route{Name: "AppShare", Method: "POST", Pattern: handlers.NotePrefix + ":id/share", HandlerFunc: handler.AppShare, Web: true, UseSession: true},
		Route{Name: "AppPin", Method: "POST", Pattern: handlers.NotePrefix + ":id/pin", HandlerFunc: handler.AppPin, Web: true, UseSession: true},
		Route{Name: "AppHistory", Method: "GET", Pattern: handlers.NotePrefix + ":id/history", HandlerFunc: handler.AppHistory, Web: true, UseSession: true},
		Route{Name: "AppRestore", Method: "POST", Pattern: handlers.NotePrefix + ":id/history", HandlerFunc: handler.AppRestore, Web: true, UseSession: true},
		Route{Name: "AppExport", Method: "GET", Pattern: handlers.ExportPath, HandlerFunc: handler.ExportNotes, Web: true, UseSession: true},
		Route{Name: "AppImport", Method: "POST", Pattern: handlers.ImportPath, HandlerFunc: handler.AppImport, Web: true, UseSession: true},
		// A shared note's public page (#83): no session, the token is the key.
		Route{Name: "Shared", Method: "GET", Pattern: handlers.SharedPath(":token"), HandlerFunc: handler.Shared, Web: true},
		Route{Name: "AppNew", Method: "GET", Pattern: handlers.NewPath, HandlerFunc: handler.AppNew, Web: true, UseSession: true},
		Route{Name: "AppPreview", Method: "POST", Pattern: handlers.PreviewPath, HandlerFunc: handler.AppPreview, Web: true, UseSession: true},
		Route{Name: "AppCreate", Method: "POST", Pattern: handlers.NewPath, HandlerFunc: handler.AppCreate, Web: true, UseSession: true},
		// *title, not :title: a title may hold a '/' (escaped as %2F).
		Route{Name: "AppTitle", Method: "GET", Pattern: handlers.TitlePrefix + "*title", HandlerFunc: handler.AppTitle, Web: true, UseSession: true},
	}
}
