// +build !race

/*
 * Copyright 2019 Banco Bilbao Vizcaya Argentaria, S.A.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package mux

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/BBVA/kapow/internal/server/data"
	"github.com/BBVA/kapow/internal/server/model"
	"github.com/BBVA/kapow/internal/server/user/spawn"
)

func TestHandlerBuilderCallsSpawner(t *testing.T) {
	route := model.Route{}
	idGenerator = uuid.NewUUID
	called := false
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		called = true
		return nil
	}

	handlerBuilder(route).ServeHTTP(nil, nil)

	if !called {
		t.Error("Didn't call spawner")
	}
}

func TestHandlerBuilderStoresHandlerInDataHandlers(t *testing.T) {
	route := model.Route{}
	added := false
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		added = len(data.Handlers.ListIDs()) != 0

		return nil
	}
	h := handlerBuilder(route)
	data.Handlers = data.New()

	h.ServeHTTP(nil, nil)

	if !added {
		t.Error("handler not added to data.Handlers")
	}
}

func TestHandlerBuilderStoresTheProperRoute(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{
		ID: "foo",
	}
	var got model.Route
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		hid := data.Handlers.ListIDs()[0]
		handler, _ := data.Handlers.Get(hid)
		got = handler.Route

		return nil
	}

	handlerBuilder(route).ServeHTTP(nil, nil)

	if !reflect.DeepEqual(got, route) {
		t.Error("Route not stored properly in the handler")
	}
}

func TestHandlerBuilderStoresTheProperRequest(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{}
	var got *http.Request
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		hid := data.Handlers.ListIDs()[0]
		handler, _ := data.Handlers.Get(hid)
		got = handler.Request

		return nil
	}
	r := &http.Request{}

	handlerBuilder(route).ServeHTTP(nil, r)

	if got != r {
		t.Error("Request not stored properly in the handler")
	}
}

func TestHandlerBuilderStoresTheProperResponseWriter(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{}
	var got http.ResponseWriter
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		hid := data.Handlers.ListIDs()[0]
		handler, _ := data.Handlers.Get(hid)
		got = handler.Writer

		return nil
	}
	w := httptest.NewRecorder()
	w.Flushed = !w.Flushed

	handlerBuilder(route).ServeHTTP(w, nil)

	if !reflect.DeepEqual(got, w) {
		t.Error("ResponseWriter not stored properly in the handler")
	}
}

func TestHandlerBuilderGeneratesAProperID(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{}
	var got string
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		hid := data.Handlers.ListIDs()[0]
		handler, _ := data.Handlers.Get(hid)
		got = handler.ID

		return nil
	}

	handlerBuilder(route).ServeHTTP(nil, nil)

	if _, err := uuid.Parse(got); err != nil {
		t.Error("ID not generated properly")
	}
}

func TestHandlerBuilderCallsSpawnerWithTheStoredHandler(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{}
	var gotStored *model.Handler
	var gotPassed *model.Handler
	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		gotPassed = h
		hid := data.Handlers.ListIDs()[0]
		gotStored, _ = data.Handlers.Get(hid)

		return nil
	}

	handlerBuilder(route).ServeHTTP(nil, nil)

	if gotStored != gotPassed {
		t.Error("Proper handler not passed to spawner()")
	}
}

func TestHandlerBuilder500sWhenIDGeneratorFails(t *testing.T) {
	data.Handlers = data.New()
	spawner = spawn.Spawn
	route := model.Route{}
	w := httptest.NewRecorder()
	idGenerator = func() (uuid.UUID, error) {
		var uuid uuid.UUID
		return uuid, errors.New(
			"End of Time reached; Try again before, or in the next Big Bang cycle")
	}

	handlerBuilder(route).ServeHTTP(w, nil)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Error("ID generation failure not handled gracefully")
	}
}

func TestHandlerBuilderRemovesHandlerWhenDone(t *testing.T) {
	data.Handlers = data.New()
	spawner = spawn.Spawn
	idGenerator = uuid.NewUUID
	route := model.Route{}

	handlerBuilder(route).ServeHTTP(nil, nil)

	if len(data.Handlers.ListIDs()) != 0 {
		t.Error("Handler not removed upon completion")
	}
}

func TestHandlerBuilderLogToLogHandlerWhenDebugIsEnabled(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{Debug: true}
	var got string

	logHandler = new(bytes.Buffer)

	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		_, _ = out.Write([]byte("this is stdout"))
		_, _ = er.Write([]byte("this is stderr"))

		return nil
	}

	handlerBuilder(route).ServeHTTP(nil, nil)

	// NOTE: logStream will write stdout and stderr contents eventually.
	// We do not have any control the goroutines running logStream, thus we
	// cannot use a synchronization primitive to wait for them.  Sorry.
	time.Sleep(1 * time.Second)

	got = logHandler.(*bytes.Buffer).String()
	if ! strings.Contains(got, "this is stdout") {
		t.Errorf("Stdout not preserved. Actual: %+q", got)
	}
	if ! strings.Contains(got, "this is stderr") {
		t.Errorf("Stderr not preserved. Actual: %+q", got)
	}
}


func TestHandlerBuilderDoesNotLogToLogHandlerWhenDebugIsDisabled(t *testing.T) {
	data.Handlers = data.New()
	route := model.Route{Debug: false}

	logHandler = new(bytes.Buffer)

	spawner = func(h *model.Handler, out io.Writer, er io.Writer) error {
		if out != nil {
			_, _ = out.Write([]byte("this is stdout"))
		}
		if er != nil {
			_, _ = er.Write([]byte("this is stderr"))
		}

		return nil
	}

	handlerBuilder(route).ServeHTTP(nil, nil)

	// NOTE: logStream will write stdout and stderr contents eventually.
	// We do not have any control the goroutines running logStream, thus we
	// cannot use a synchronization primitive to wait for them.  Sorry.
	time.Sleep(1 * time.Second)

	size := logHandler.(*bytes.Buffer).Len()
	if size != 0 {
		t.Error("Something was logged to stderr with debug=false")
	}
}
