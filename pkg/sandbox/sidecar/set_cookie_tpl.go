package sidecar

import (
	corev1 "k8s.io/api/core/v1"
)

func getHTTProbePath(pod *corev1.Pod) (probesPath []string) {
	for _, c := range pod.Spec.Containers {
		if c.LivenessProbe != nil && c.LivenessProbe.HTTPGet != nil {
			probesPath = append(probesPath, c.LivenessProbe.HTTPGet.Path)
		}
		if c.ReadinessProbe != nil && c.ReadinessProbe.HTTPGet != nil {
			probesPath = append(probesPath, c.ReadinessProbe.HTTPGet.Path)
		}
		if c.StartupProbe != nil && c.StartupProbe.HTTPGet != nil {
			probesPath = append(probesPath, c.StartupProbe.HTTPGet.Path)
		}
	}
	return probesPath
}

const envoySetCookie = `
local pattern = "Domain=([^;]*)"
function split(str, sep)
    local ret = {}
    for s in string.gmatch(str, "([^"..sep.."]+)") do
        table.insert(ret, s)
    end
    return ret
end
function replace(str, pattern, replacement)
    return string.gsub(str, pattern, replacement)
end

function match_and_reset_cookie(set_cookie, host)
    local ret_str = ""
    -- split set-cookie-string with ',' due this string may be like <a=b;example.com,c=d;Domain=example.com>
    cookies = split(set_cookie, ",")
    for k, cookie in ipairs(cookies) do
        reset_cookie = cookie
        -- get domain from set-cookie-string
        set_cookie_domain = string.match(cookie, pattern)
        if set_cookie_domain == nil or set_cookie_domain == "" then
        else
			reset_cookie = replace(cookie, pattern, "Domain=")
        end
        -- concat modified set-cookie-string
        if string.len(ret_str) > 0 then
            ret_str = ret_str .. "," .. reset_cookie
        else
            ret_str = reset_cookie
        end
    end
    return ret_str
end

local x_forwarded_host = ""
function envoy_on_request(request_handle)
    x_forwarded_host = request_handle:headers():get("X-Forwarded-Host")
end
function envoy_on_response(response_handle)
	local set_cookie = response_handle:headers():get("Set-Cookie")
	if set_cookie ~= nil then
		response_handle:logInfo("setCookie: "..set_cookie)
		response_handle:logInfo("xForwardedHost: "..x_forwarded_host)
	end
    if set_cookie == nil or set_cookie == "" or x_forwarded_host == "" then
		return
    end
	-- Set-Cookie: <cookie-name>=<cookie-value>; Domain=<domain-value>
    -- Set-Cookie: a=b;Domain=example.com,c=d;Secure   
	set_cookie_str = match_and_reset_cookie(set_cookie, x_forwarded_host)
    response_handle:headers():replace("Set-Cookie", set_cookie_str)
end
`

func genEnvoySetCookieScript() []byte {
	return []byte(envoySetCookie)
}
