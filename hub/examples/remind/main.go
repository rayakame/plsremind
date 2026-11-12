// Command remind is a minimal reminder adapter. It connects to the gateway,
// prints every reminder it is given and confirms it.
//
//	PLSREMIND_URL=https://todo.example.com PLSREMIND_TOKEN=plsr_pat_... go run ./examples/remind
//
// The token must be a personal token marked for reminders, or a connection token.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
)

const (
	opDispatch     = 0
	opHeartbeat    = 1
	opIdentify     = 2
	opReconnect    = 7
	opHello        = 10
	closeAuthFail  = 4004
	closeBadIntent = 4013
	closeNoIntent  = 4014
)

type frame struct {
	Op int             `json:"op"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}

type delivery struct {
	UserID string `json:"user_id"`
	Data   struct {
		ID       string `json:"id"`
		Attempt  int    `json:"attempt"`
		Late     bool   `json:"late"`
		Reminder struct {
			Kind         string    `json:"kind"`
			Offset       string    `json:"offset"`
			ScheduledFor time.Time `json:"scheduled_for"`
		} `json:"reminder"`
		Item struct {
			Title string     `json:"title"`
			DueAt *time.Time `json:"due_at"`
		} `json:"item"`
		User struct {
			DisplayName string   `json:"display_name"`
			ExternalIDs []string `json:"external_ids"`
		} `json:"user"`
	} `json:"data"`
}

var errDone = errors.New("done")

func main() {
	once := flag.Bool("once", false, "exit after the first reminder")
	flag.Parse()
	base := strings.TrimRight(os.Getenv("PLSREMIND_URL"), "/")
	token := os.Getenv("PLSREMIND_TOKEN")
	if base == "" || token == "" {
		log.Fatal("set PLSREMIND_URL and PLSREMIND_TOKEN")
	}

	for {
		err := run(base, token, *once)
		if errors.Is(err, errDone) {
			return
		}
		var closed *websocket.CloseError
		if errors.As(err, &closed) && (closed.Code == closeAuthFail || closed.Code == closeBadIntent || closed.Code == closeNoIntent) {
			log.Fatalf("the hub refused the connection: %v", err)
		}
		// Deliveries that were not confirmed come again after every login,
		// so a plain reconnect is all it takes to not lose any.
		log.Printf("connection lost (%v), reconnecting", err)
		time.Sleep(2 * time.Second)
	}
}

func gatewayURL(base string) (string, error) {
	res, err := http.Get(base + "/api/v1/info")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var info struct {
		GatewayURL string `json:"gateway_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.GatewayURL, nil
}

func run(base, token string, once bool) error {
	url, err := gatewayURL(base)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var writeMu sync.Mutex
	send := func(op int, d any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(map[string]any{"op": op, "d": d})
	}
	stop := make(chan struct{})
	defer close(stop)

	for {
		var f frame
		if err := conn.ReadJSON(&f); err != nil {
			return err
		}
		switch {
		case f.Op == opHello:
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			if err := json.Unmarshal(f.D, &hello); err != nil {
				return err
			}
			go heartbeat(send, time.Duration(hello.HeartbeatInterval)*time.Millisecond, stop)
			if err := send(opIdentify, map[string]any{"token": token, "intents": []string{"deliveries"}}); err != nil {
				return err
			}
		case f.Op == opReconnect:
			return errors.New("the hub asked to reconnect")
		case f.Op == opDispatch && f.T == "READY":
			log.Print("connected, waiting for reminders")
		case f.Op == opDispatch && f.T == "delivery.created":
			var d delivery
			if err := json.Unmarshal(f.D, &d); err != nil {
				return err
			}
			remind(d)
			// Confirm only now that the reminder has reached the person.
			if err := acknowledge(base, token, d.Data.ID); err != nil {
				return err
			}
			if once {
				return errDone
			}
		case f.Op == opDispatch && f.T == "delivery.closed":
			log.Printf("a reminder was withdrawn: %s", f.D)
		}
	}
}

func heartbeat(send func(int, any) error, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if send(opHeartbeat, nil) != nil {
				return
			}
		}
	}
}

func remind(d delivery) {
	what := map[string]string{
		"before":    "is due in " + d.Data.Reminder.Offset,
		"due":       "is due now",
		"follow_up": "is overdue",
		"manual":    "you asked to be reminded",
	}[d.Data.Reminder.Kind]
	late := ""
	if d.Data.Late {
		late = fmt.Sprintf(" (was meant for %s)", d.Data.Reminder.ScheduledFor.Local().Format("15:04"))
	}
	fmt.Printf("%s: %q %s%s\n", d.Data.User.DisplayName, d.Data.Item.Title, what, late)
}

func acknowledge(base, token, id string) error {
	body, _ := json.Marshal(map[string]string{"status": "delivered"})
	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/deliveries/"+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("confirming delivery %s: status %d", id, res.StatusCode)
	}
	return nil
}
