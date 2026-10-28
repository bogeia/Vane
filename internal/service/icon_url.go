package service

import "net/url"

func sourceIconURL(id, version string) string {
	if version == "" {
		return ""
	}
	return "/api/source-icons/" + url.PathEscape(id) + "?v=" + url.QueryEscape(version)
}
