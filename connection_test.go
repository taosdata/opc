package opc

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var testLogger *logrus.Entry

var connConfig = DefaultConnectionConfig()

func TestMain(m *testing.M) {
	baseLogger := logrus.New()
	baseLogger.SetLevel(logrus.DebugLevel)
	testLogger = baseLogger.WithField("test", "opcda_test")
	code := m.Run()
	OleRelease()
	os.Exit(code)
}

func TestOPCBrowser(t *testing.T) {
	browser, err := CreateBrowser(
		"Graybox.Simulator",
		[]string{"localhost"},
		testLogger,
	)
	if err != nil {
		t.Fatal(err)
	}
	if browser.Name != "root" {
		t.Fatal("structure of browser tree is compromised: root")
	}
	if browser.Branches[0].Name != "options" {
		t.Fatal("structure of browser tree is compromised: options")
	}
	if len(browser.Branches[0].Leaves) != 4 {
		t.Fatal("structure of browser tree is compromised: number of leaves for options")
	}
}

func TestOPCBrowserWrongServer(t *testing.T) {
	browser, err := CreateBrowser(
		"Graybox.Simulator.NOTREAL",
		[]string{"localhost"},
		testLogger,
	)
	assert.Error(t, err)
	assert.Nil(t, browser)
}

func TestNewConnectionNoTags(t *testing.T) {
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{},
		connConfig,
		testLogger,
	)
	client.Close()
}

func TestNewConnectionWithTags(t *testing.T) {
	client, err := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{"numeric.sin.int64", "numeric.saw.float"},
		connConfig,
		testLogger,
	)
	assert.NoError(t, err)
	client.Close()
}

func TestNewConnectionWrongServer(t *testing.T) {
	client, err := NewConnection(
		"Graybox.Simulator.NOTREAL",
		[]string{"localhost"},
		[]string{},
		connConfig,
		testLogger,
	)
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestNewConnectionWrongNode(t *testing.T) {
	client, err := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost.NOTREAL"},
		[]string{},
		connConfig,
		testLogger,
	)
	assert.Error(t, err)
	assert.Nil(t, client)

}

func TestNewConnectionWrongTags(t *testing.T) {
	client, err := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{"numeric.sin.int64.NOTREAL"},
		connConfig,
		testLogger,
	)
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestAddTags(t *testing.T) {
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{},
		connConfig,
		testLogger,
	)
	defer client.Close()
	err := client.Add("numeric.sin.int64", "numeric.saw.float")
	assert.NoError(t, err)
}

func TestAddWrongTag(t *testing.T) {
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{},
		connConfig,
		testLogger,
	)
	defer client.Close()
	err := client.Add("numeric.sin.int64.NOTREAL")
	assert.Error(t, err)
}

func TestRemoveTags(t *testing.T) {
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{"numeric.sin.int64", "numeric.saw.float"},
		connConfig,
		testLogger,
	)
	defer client.Close()
	client.Remove("numeric.sin.int64")
	client.Remove("numeric.saw.float")
}

func TestGetTags(t *testing.T) {
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{},
		connConfig,
		testLogger,
	)
	defer client.Close()
	var config = []struct {
		Add    []string
		Remove []string
		Want   []string
	}{
		{
			Add:  []string{"numeric.sin.float"},
			Want: []string{"numeric.sin.float"},
		},
		{
			Remove: []string{"numeric.sin.float"},
			Want:   nil,
		},
		{
			Add:  []string{"numeric.saw.float"},
			Want: []string{"numeric.saw.float"},
		},
		{
			Remove: []string{"numeric.saw.float", "numeric.sin.float"},
			Want:   nil,
		},
	}

	for _, cfg := range config {
		if cfg.Add != nil {
			err := client.Add(cfg.Add...)
			assert.NoError(t, err)
		}
		if cfg.Remove != nil {
			for _, tag := range cfg.Remove {
				client.Remove(tag)
			}
		}
		tags := client.Tags()
		assert.Equal(t, cfg.Want, tags)
	}
}

func TestTags(t *testing.T) {
	var want []string
	client := &opcConnectionImpl{}
	tags := client.Tags()
	assert.Equal(t, want, tags)
}

func TestAutomationItemsClose(t *testing.T) {
	conn := &opcConnectionImpl{}
	conn.Items.Close()
}

func TestOpcRead(t *testing.T) {
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{"numeric.sin.int64", "numeric.saw.float"},
		connConfig,
		testLogger,
	)
	defer client.Close()

	// read all added tags (items)
	var m map[string]Item
	for i := 0; i < 10; i++ {
		m = client.Read()
		assert.Equal(t, 2, len(m))
		if len(m) != 2 {
			t.Fatal("the map should have only two items")
		}
		quality := int16(0)
		for _, item := range m {
			quality |= item.Quality
		}
		if quality != 192 {
			t.Log(quality)
			time.Sleep(time.Second)
			continue
		}
		break
	}

	keys := map[string]struct{}{"numeric.sin.int64": {}, "numeric.saw.float": {}}
	for key, item := range m {
		assert.NotNil(t, item.Value)
		assert.Contains(t, keys, key)
		delete(keys, key)
		t.Log(item.Quality)
		assert.Equal(t, int16(192), item.Quality)
	}
	assert.Equal(t, 0, len(keys))
}

func Test_ensureInt16(t *testing.T) {
	type args struct {
		q interface{}
	}
	tests := []struct {
		name string
		args args
		want int16
	}{
		{name: "int16 input", args: args{q: int16(192)}, want: int16(192)},
		{name: "int32 input", args: args{q: int32(192)}, want: int16(192)},
		{name: "int64 input", args: args{q: int64(192)}, want: int16(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, ensureInt16(tt.args.q), "ensureInt16(%v)", tt.args.q)
		})
	}
}

func TestReconnect(t *testing.T) {
	client, err := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{"numeric.sin.int64", "numeric.saw.float"},
		connConfig,
		testLogger,
	)
	assert.NoError(t, err)
	impl := client.(*opcConnectionImpl)
	err = KillProcessByName("gb_opcsim.exe")
	assert.NoError(t, err)
	impl.fix()
}

func KillProcessByName(name string) error {
	cmd := exec.Command("taskkill", "/IM", name, "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill failed: %v: %s", err, string(out))
	}
	return nil
}
