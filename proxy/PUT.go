/**
* Copyright © 2017, ACM@UIUC
*
* This file is part of the Groot Project.
*
* The Groot Project is open source software, released under the University of
* Illinois/NCSA Open Source License. You should have received a copy of
* this license in a file with the distribution.
**/

package proxy

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/arbor-dev/arbor/logger"
)

// PUT provides a proxy PUT request allowing authorized clients to make PUT
// requests of the microservices
//
// Pass the http Request from the client and the ResponseWriter it expects.
//
// Pass the target url of the backend service (not the url the client called).
//
// Pass the format of the service.
//
// Pass a authorization token (optional).
//
// Will call the service and return the result to the client.
func PUT(w http.ResponseWriter, url string, format string, token string, r *http.Request) {

	origin := r.Header.Get("Origin")

	//TODO: FIGURE OUT ORIGIN RULES
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "PUT")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
	}

	preprocessing_err := requestPreprocessing(w, r)
	if preprocessing_err != nil {
		return
	}

	if format == "RAW" {
		rawPUT(r, w, url, token)
		return
	}

	if format != "XML" && format != "JSON" { //TODO: Support Post form data
		err := errors.New("Unsupported data encoding")
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}
	content, err := ioutil.ReadAll(io.LimitReader(r.Body, MaxRequestSize))
	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}
	if err := r.Body.Close(); err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed Reception:%v", err))
		return
	}

	var data interface{}
	err = json.Unmarshal(content, &data)
	if err != nil {
		invalidPOST(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}

	switch format {
	case "XML":
		xmlPUT(r, w, url, token, data)
		return
	case "JSON":
		jsonPUT(r, w, url, token, data)
		return
	default:
		invalidPUT(w, err)
		logger.Log(logger.ERR, "Unsupported Data Encoding")
		return
	}
}

// For PUT with actual processing errors.
// For unsuccessful PUT requests, use unsuccessfulPUT
func invalidPUT(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	data := map[string]interface{}{"Code": http.StatusUnprocessableEntity, "Text": "Unprocessable Entity", "Specfically": err}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Log(logger.ERR, err.Error())
		notifyClientOfRequestError(w, http.StatusBadGateway, "") //ASK: SHOULD THIS BE HERE?
	}
}

// If the PUT was simply unsuccessful
func unsuccessfulPUT(w http.ResponseWriter, format string, content []byte, err error) {
	var data interface{}
	switch format {
	case "JSON":
		json.Unmarshal(content, &data)
		break
	case "XML":
		xml.Unmarshal(content, &data)
		break
	default:
		// Text / HTML
		w.Write(content)
		return
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Log(logger.ERR, err.Error())
		notifyClientOfRequestError(w, http.StatusBadGateway, "")
	}
}

func jsonPUT(r *http.Request, w http.ResponseWriter, url string, token string, data interface{}) {
	content, err := json.Marshal(data)
	if err != nil {
		invalidPOST(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(content))

	for k, vs := range r.Header {
		req.Header[k] = make([]string, len(vs))
		copy(req.Header[k], vs)
	}
	req.Header.Set("Content-Type", JSONHeader)
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	client := &http.Client{
		Timeout: time.Duration(Timeout) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	logger.LogResp(logger.DEBUG, resp)

	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	} else if resp.StatusCode == http.StatusFound {
		logger.Log(logger.DEBUG, "Service Returned Redirect")
		w.Header().Set("Location", resp.Header.Get("Location"))
		w.WriteHeader(http.StatusFound)
		return
	} else if resp.StatusCode != http.StatusOK {
		logger.Log(logger.WARN, "SERVICE FAILED - SERVICE RETURNED STATUS "+http.StatusText(resp.StatusCode))

		w.Header().Set("Content-Type", "application/json; charset=UTF-8")

		w.WriteHeader(resp.StatusCode)

		content, readErr := ioutil.ReadAll(resp.Body)

		unsuccessfulPUT(w, "JSON", content, readErr)

		return
	}

	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)
	var serverData interface{}
	err = json.Unmarshal(contents, &serverData)
	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed to decode:%v", err))
		return
	}

	w.Header().Set("Content-Type", JSONHeader)

	if err := json.NewEncoder(w).Encode(serverData); err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed to encode:%v", err))
		return
	}

	//ADD BACK IF THINGS START BREAKING
	//w.WriteHeader(http.StatusOK)
}

func xmlPUT(r *http.Request, w http.ResponseWriter, url string, token string, data interface{}) {
	content, err := xml.Marshal(data)
	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(content))
	req.Header.Set("Content-Type", XMLHeader)

	for k, vs := range r.Header {
		req.Header[k] = make([]string, len(vs))
		copy(req.Header[k], vs)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	client := &http.Client{
		Timeout: time.Duration(Timeout) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	logger.LogResp(logger.DEBUG, resp)

	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	} else if resp.StatusCode == http.StatusFound {
		logger.Log(logger.DEBUG, "Service Returned Redirect")
		w.Header().Set("Location", resp.Header.Get("Location"))
		w.WriteHeader(http.StatusFound)
		return
	} else if resp.StatusCode != http.StatusOK {
		logger.Log(logger.WARN, "SERVICE FAILED - SERVICE RETURNED STATUS "+http.StatusText(resp.StatusCode))

		w.Header().Set("Content-Type", XMLHeader)

		w.WriteHeader(resp.StatusCode)

		contents, readErr := ioutil.ReadAll(resp.Body)

		unsuccessfulPUT(w, "XML", contents, readErr)

		return
	}

	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)
	var serverData interface{}
	err = xml.Unmarshal(contents, &serverData)
	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed decode:%v", err))
		return
	}

	w.Header().Set("Content-Type", JSONHeader)

	if err := json.NewEncoder(w).Encode(serverData); err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed encode:%v", err))
		return
	}

	w.WriteHeader(http.StatusOK)
}

func rawPUT(r *http.Request, w http.ResponseWriter, url string, token string) {
	content, err := ioutil.ReadAll(io.LimitReader(r.Body, MaxFileUploadSize))

	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}

	if err := r.Body.Close(); err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed Reception:%v", err))
		return
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(content))

	for k, vs := range r.Header {
		req.Header[k] = make([]string, len(vs))
		copy(req.Header[k], vs)
	}

	if token != "" {
		req.Header.Set("Authorization", token)
	}

	client := &http.Client{Timeout: time.Duration(Timeout) * time.Second}
	resp, err := client.Do(req)
	logger.LogResp(logger.DEBUG, resp)

	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	} else if resp.StatusCode != http.StatusOK {
		logger.Log(logger.WARN, "SERVICE FAILED - SERVICE RETURNED STATUS "+http.StatusText(resp.StatusCode))
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(resp.StatusCode)
	}

	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, err.Error())
		return
	}

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))

	_, err = w.Write(contents)

	if err != nil {
		invalidPUT(w, err)
		logger.Log(logger.ERR, fmt.Sprintf("Failed to Write:%v", err))
		return
	}
}
