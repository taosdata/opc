package opc

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
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
	client := &OpcConnectionImpl{}
	tags := client.Tags()
	assert.Equal(t, want, tags)
}

func TestAutomationItemsClose(t *testing.T) {
	conn := &OpcConnectionImpl{}
	conn.Items.Close()
}

func TestOpcRead(t *testing.T) {
	points := []string{
		"numeric.triangle.int8",
		"numeric.triangle.int16",
		"numeric.triangle.int32",
		"numeric.triangle.int64",
		"numeric.triangle.uint8",
		"numeric.triangle.uint16",
		"numeric.triangle.uint32",
		"numeric.triangle.uint64",
		"numeric.triangle.float",
		"numeric.triangle.double",
	}
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		points,
		connConfig,
		testLogger,
	)
	defer client.Close()

	// read all added tags (items)
	var m map[string]Item
	for i := 0; i < 10; i++ {
		m = client.Read()
		assert.Equal(t, len(points), len(m))
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
	// check all points are read
	keys := make(map[string]struct{}, len(points))
	for i := 0; i < len(points); i++ {
		keys[points[i]] = struct{}{}
	}
	for key, item := range m {
		assert.NotNil(t, item.Value)
		assert.Contains(t, keys, key)
		delete(keys, key)
		t.Log(item.Quality)
		assert.Equal(t, int16(192), item.Quality)
	}
	assert.Equal(t, 0, len(keys))
	t.Logf("%T", m["numeric.triangle.int8"].Value)
	t.Logf("%T", m["numeric.triangle.int16"].Value)
	t.Logf("%T", m["numeric.triangle.int32"].Value)
	t.Logf("%T", m["numeric.triangle.int64"].Value)
	t.Logf("%T", m["numeric.triangle.uint8"].Value)
	t.Logf("%T", m["numeric.triangle.uint16"].Value)
	t.Logf("%T", m["numeric.triangle.uint32"].Value)
	t.Logf("%T", m["numeric.triangle.uint64"].Value)
	intVal := m["numeric.triangle.int8"].Value.(int16)
	assert.NotEqual(t, int16(0), intVal)
	uintVal := m["numeric.triangle.uint8"].Value.(uint8)
	assert.NotEqual(t, uint8(0), uintVal)
	floatVal := m["numeric.triangle.float"].Value.(float32)
	assert.NotEqual(t, float32(0), floatVal)
	doubleVal := m["numeric.triangle.double"].Value.(float64)
	assert.NotEqual(t, float64(0), doubleVal)
	t.Logf("int8: %d, uint8: %d, float: %f, double: %f", intVal, uintVal, floatVal, doubleVal)
	assert.InDelta(t, float64(floatVal), doubleVal, 0.0001)
	//int val
	assert.Equal(t, int16(intVal), m["numeric.triangle.int16"].Value.(int16))
	assert.Equal(t, int32(intVal), m["numeric.triangle.int32"].Value.(int32))
	assert.Equal(t, int64(intVal), m["numeric.triangle.int64"].Value.(int64))
	//uint val
	assert.Equal(t, int32(uintVal), m["numeric.triangle.uint16"].Value.(int32))
	assert.Equal(t, float64(uintVal), m["numeric.triangle.uint32"].Value.(float64))
	assert.Equal(t, uint64(uintVal), m["numeric.triangle.uint64"].Value.(uint64))
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
	impl := client.(*OpcConnectionImpl)
	err = KillProcessByName("gb_opcsim.exe")
	assert.NoError(t, err)
	impl.Fix(false)
}

func KillProcessByName(name string) error {
	cmd := exec.Command("taskkill", "/IM", name, "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill failed: %v: %s", err, string(out))
	}
	return nil
}

func TestReconnectForce(t *testing.T) {
	connConfig := DefaultConnectionConfig()
	connConfig.FailedReadsToForceReconnect = 1
	client, err := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		[]string{"numeric.sin.int64", "numeric.saw.float"},
		connConfig,
		testLogger,
	)
	defer client.Close()
	assert.NoError(t, err)
	err = KillProcessByName("gb_opcsim.exe")
	assert.NoError(t, err)
	values := client.Read()
	assert.Equal(t, 1, len(values))
	time.Sleep(time.Second * 2)
	values = client.Read()
	assert.Equal(t, 2, len(values))
}

func TestCacheTags(t *testing.T) {
	points := []string{
		"numeric.triangle.int8",
		"numeric.triangle.int16",
		"numeric.triangle.int32",
		"numeric.triangle.int64",
		"numeric.triangle.uint8",
		"numeric.triangle.uint16",
		"numeric.triangle.uint32",
		"numeric.triangle.uint64",
		"numeric.triangle.float",
		"numeric.triangle.double",
	}
	sort.Strings(points)
	client, _ := NewConnection(
		"Graybox.Simulator",
		[]string{"localhost"},
		points,
		connConfig,
		testLogger,
	)
	defer client.Close()
	// read all added tags (items)
	values := client.Read()
	assert.Equal(t, len(points), len(values))

	// remove tag
	client.Remove("numeric.triangle.int8")
	tags := client.Tags()
	assert.Equal(t, len(points)-1, len(tags))
	wantRemoveTags := make([]string, 0, len(points)-1)
	for _, tag := range points {
		if tag != "numeric.triangle.int8" {
			wantRemoveTags = append(wantRemoveTags, tag)
		}
	}
	sort.Strings(wantRemoveTags)
	sort.Strings(tags)
	assert.Equal(t, wantRemoveTags, tags)
	// read all added tags (items)
	values = client.Read()
	assert.Equal(t, len(points)-1, len(values))
	gotTags := make([]string, 0, len(values)-1)
	for tag := range values {
		gotTags = append(gotTags, tag)
	}
	sort.Strings(gotTags)
	assert.Equal(t, wantRemoveTags, gotTags)
	// add tag back
	err := client.Add("numeric.triangle.int8")
	assert.NoError(t, err)
	tags = client.Tags()
	assert.Equal(t, len(points), len(tags))
	sort.Strings(tags)
	assert.Equal(t, points, tags)
	// read all added tags (items)
	values = client.Read()
	assert.Equal(t, len(points), len(values))
	gotTags = make([]string, 0, len(values))
	for tag := range values {
		gotTags = append(gotTags, tag)
	}
	sort.Strings(gotTags)
	assert.Equal(t, points, gotTags)
	// remove tag again
	client.Remove("numeric.triangle.int8")
	tags = client.Tags()
	sort.Strings(tags)
	assert.Equal(t, wantRemoveTags, tags)
	// read all added tags (items)
	values = client.Read()
	assert.Equal(t, len(points)-1, len(values))
	gotTags = make([]string, 0, len(values)-1)
	for tag := range values {
		gotTags = append(gotTags, tag)
	}
	sort.Strings(gotTags)
	assert.Equal(t, wantRemoveTags, gotTags)
	// reconnect
	err = KillProcessByName("gb_opcsim.exe")
	assert.NoError(t, err)
	values = client.Read()
	assert.Equal(t, len(points)-2, len(values))
	values = client.Read()
	assert.Equal(t, len(points)-1, len(values))
	gotTags = make([]string, 0, len(values)-1)
	for tag := range values {
		gotTags = append(gotTags, tag)
	}
	sort.Strings(gotTags)
	assert.Equal(t, wantRemoveTags, gotTags)
	// add tag back
	err = client.Add("numeric.triangle.int8")
	assert.NoError(t, err)
	tags = client.Tags()
	assert.Equal(t, len(points), len(tags))
	sort.Strings(tags)
	assert.Equal(t, points, tags)
	// read all added tags (items)
	values = client.Read()
	// finally, check tags
	valueTags := make([]string, 0, len(values))
	for tag := range values {
		valueTags = append(valueTags, tag)
	}
	sort.Strings(valueTags)
	assert.Equal(t, points, valueTags)
	// get tags
	tags = client.Tags()
	assert.Equal(t, len(points), len(tags))
	sort.Strings(tags)
	assert.Equal(t, points, tags)
}
