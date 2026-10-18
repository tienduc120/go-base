package rest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/services/example/internal/application"
	"github.com/tienduc120/go-base/services/example/internal/interfaces/rest"
)

type fakeCreator struct {
	got    application.CreateNoteCommand
	result application.CreateNoteResult
	err    error
}

func (f *fakeCreator) Execute(
	_ context.Context,
	cmd application.CreateNoteCommand,
) (application.CreateNoteResult, error) {
	f.got = cmd
	return f.result, f.err
}

func post(t *testing.T, creator rest.Creator, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/notes", strings.NewReader(body))
	rec := httptest.NewRecorder()
	rest.NewHandler(creator).ServeHTTP(rec, req)
	return rec
}

func TestCreateReturns201WithTheNewID(t *testing.T) {
	t.Parallel()

	creator := &fakeCreator{result: application.CreateNoteResult{ID: "id-1"}}
	rec := post(t, creator, `{"id":"id-1","name":"first"}`)

	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"id":"id-1"`) {
		t.Errorf("got %d %s, want 201 with the id", rec.Code, rec.Body.String())
	}
	if creator.got.ID != "id-1" || creator.got.Name != "first" {
		t.Errorf("the use case received %+v, want what the body said", creator.got)
	}
}

func TestCreateMapsFailuresToStatusCodes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		body string
		err  error
		want int
	}{
		"malformed body":  {body: `{`, want: http.StatusBadRequest},
		"invalid command": {body: `{}`, err: application.ErrInvalidCommand, want: http.StatusBadRequest},
		"already exists":  {body: `{}`, err: application.ErrAlreadyExists, want: http.StatusConflict},
		"storage failure": {
			body: `{}`,
			err:  errors.New("dial tcp 10.0.0.5:5432: connection refused"),
			want: http.StatusInternalServerError,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := post(t, &fakeCreator{err: tc.err}, tc.body)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if strings.Contains(rec.Body.String(), "10.0.0.5") {
				t.Errorf("an internal error must not reach the client: %s", rec.Body.String())
			}
		})
	}
}

func TestCreateRefusesAnOversizedBody(t *testing.T) {
	t.Parallel()

	huge := `{"id":"id-1","name":"` + strings.Repeat("x", 2<<20) + `"}`
	if rec := post(t, &fakeCreator{}, huge); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: a body past the cap must not be read into memory", rec.Code)
	}
}
