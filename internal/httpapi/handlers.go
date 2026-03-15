package httpapi

import (
	"encoding/base64"
	"io"
	"net/http"

	"github.com/soulteary/gorge-file-storage/internal/engine"

	"github.com/labstack/echo/v4"
)

type Deps struct {
	Router *engine.Router
	Token  string
}

type apiResponse struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func RegisterRoutes(e *echo.Echo, deps *Deps) {
	e.GET("/", healthPing())
	e.GET("/healthz", healthPing())

	g := e.Group("/api/file")
	g.Use(tokenAuth(deps))

	g.POST("/upload", upload(deps))
	g.POST("/read", readFile(deps))
	g.POST("/delete", deleteFile(deps))
	g.GET("/engines", listEngines(deps))
}

func tokenAuth(deps *Deps) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if deps.Token == "" {
				return next(c)
			}
			token := c.Request().Header.Get("X-Service-Token")
			if token == "" {
				token = c.QueryParam("token")
			}
			if token == "" || token != deps.Token {
				return c.JSON(http.StatusUnauthorized, &apiResponse{
					Error: &apiError{Code: "ERR_UNAUTHORIZED", Message: "missing or invalid service token"},
				})
			}
			return next(c)
		}
	}
}

func healthPing() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
}

func respondOK(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, &apiResponse{Data: data})
}

func respondErr(c echo.Context, status int, code, msg string) error {
	return c.JSON(status, &apiResponse{
		Error: &apiError{Code: code, Message: msg},
	})
}

type uploadRequest struct {
	DataBase64 string `json:"dataBase64"`
	Engine     string `json:"engine,omitempty"`
	Name       string `json:"name,omitempty"`
	MimeType   string `json:"mimeType,omitempty"`
}

type uploadResponse struct {
	Handle   string `json:"handle"`
	Engine   string `json:"engine"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType,omitempty"`
}

func upload(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req uploadRequest
		if err := c.Bind(&req); err != nil {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
		}
		if req.DataBase64 == "" {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", "dataBase64 is required")
		}

		data, err := base64.StdEncoding.DecodeString(req.DataBase64)
		if err != nil {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", "invalid base64 data: "+err.Error())
		}

		var eng engine.StorageEngine
		if req.Engine != "" {
			eng, err = deps.Router.GetEngine(req.Engine)
			if err != nil {
				return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
			}
		} else {
			eng, err = deps.Router.SelectForWrite(int64(len(data)))
			if err != nil {
				return respondErr(c, http.StatusServiceUnavailable, "ERR_NO_ENGINE", err.Error())
			}
		}

		params := engine.WriteParams{
			Name:     req.Name,
			MimeType: req.MimeType,
		}

		handle, err := eng.WriteFile(c.Request().Context(), data, params)
		if err != nil {
			return respondErr(c, http.StatusInternalServerError, "ERR_INTERNAL", err.Error())
		}

		return respondOK(c, &uploadResponse{
			Handle:   handle,
			Engine:   eng.Identifier(),
			Size:     int64(len(data)),
			MimeType: req.MimeType,
		})
	}
}

type readRequest struct {
	Handle string `json:"handle"`
	Engine string `json:"engine"`
}

func readFile(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req readRequest
		if err := c.Bind(&req); err != nil {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
		}
		if req.Handle == "" || req.Engine == "" {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", "handle and engine are required")
		}

		eng, err := deps.Router.GetEngine(req.Engine)
		if err != nil {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
		}

		rc, err := eng.ReadFile(c.Request().Context(), req.Handle)
		if err != nil {
			return respondErr(c, http.StatusNotFound, "ERR_NOT_FOUND", err.Error())
		}
		defer func() { _ = rc.Close() }()

		data, err := io.ReadAll(rc)
		if err != nil {
			return respondErr(c, http.StatusInternalServerError, "ERR_INTERNAL", "read data: "+err.Error())
		}

		return respondOK(c, map[string]string{
			"dataBase64": base64.StdEncoding.EncodeToString(data),
		})
	}
}

type deleteRequest struct {
	Handle string `json:"handle"`
	Engine string `json:"engine"`
}

func deleteFile(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req deleteRequest
		if err := c.Bind(&req); err != nil {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
		}
		if req.Handle == "" || req.Engine == "" {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", "handle and engine are required")
		}

		eng, err := deps.Router.GetEngine(req.Engine)
		if err != nil {
			return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
		}

		if err := eng.DeleteFile(c.Request().Context(), req.Handle); err != nil {
			return respondErr(c, http.StatusInternalServerError, "ERR_INTERNAL", err.Error())
		}

		return respondOK(c, map[string]string{"status": "deleted"})
	}
}

func listEngines(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		return respondOK(c, deps.Router.ListEngines())
	}
}
