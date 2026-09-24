package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Machine readable error codes returned in the API error envelope.
// Contract: docs/API.md §1.
const (
	CodeBadRequest         = "bad_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeMethodNotAllowed   = "method_not_allowed"
	CodeConflict           = "conflict"
	CodeValidationFailed   = "validation_failed"
	CodeRateLimited        = "rate_limited"
	CodeInternalError      = "internal_error"
	CodeServiceUnavailable = "service_unavailable"
)

// ErrorBody is the payload of the "error" key.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type dataEnvelope struct {
	Data any `json:"data"`
}

// Data writes a success envelope with an explicit status code.
func Data(c *gin.Context, status int, data any) {
	c.JSON(status, dataEnvelope{Data: data})
}

// OK writes a 200 success envelope.
func OK(c *gin.Context, data any) {
	Data(c, http.StatusOK, data)
}

// Created writes a 201 success envelope.
func Created(c *gin.Context, data any) {
	Data(c, http.StatusCreated, data)
}

// Fail aborts the request with an error envelope.
func Fail(c *gin.Context, status int, code, message string, details any) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: ErrorBody{
		Code:    code,
		Message: message,
		Details: details,
	}})
}

// NotFound aborts with the standard not-found envelope. Cross-tenant access is
// always reported as not_found so resource existence never leaks.
func NotFound(c *gin.Context, message string) {
	if message == "" {
		message = "resource not found"
	}
	Fail(c, http.StatusNotFound, CodeNotFound, message, nil)
}

// Unauthorized aborts with the standard unauthorized envelope.
func Unauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "authentication required"
	}
	Fail(c, http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// Forbidden aborts with the standard forbidden envelope.
func Forbidden(c *gin.Context, message string) {
	if message == "" {
		message = "insufficient permissions"
	}
	Fail(c, http.StatusForbidden, CodeForbidden, message, nil)
}

// ValidationFailed aborts with field level details.
func ValidationFailed(c *gin.Context, message string, details any) {
	if message == "" {
		message = "request validation failed"
	}
	Fail(c, http.StatusUnprocessableEntity, CodeValidationFailed, message, details)
}
