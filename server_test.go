// +build !integration

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"./actions"
	"./proxy"
	"./server"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"time"
)

type ServerTestSuite struct {
	suite.Suite
	proxy.Service
	ConsulAddress      string
	BaseUrl            string
	ReconfigureBaseUrl string
	RemoveBaseUrl      string
	ReconfigureUrl     string
	RemoveUrl          string
	ConfigUrl          string
	CertUrl            string
	CertsUrl           string
	ResponseWriter     *ResponseWriterMock
	RequestReconfigure *http.Request
	RequestRemove      *http.Request
	InstanceName       string
	DnsIps             []string
	Server             *httptest.Server
	sd                 proxy.ServiceDest
}

func (s *ServerTestSuite) SetupTest() {
	s.sd = proxy.ServiceDest{
		ServicePath: []string{"/path/to/my/service/api", "/path/to/my/other/service/api"},
	}
	s.Service.ServiceDest = []proxy.ServiceDest{s.sd}
	s.InstanceName = "proxy-test-instance"
	s.ConsulAddress = "http://1.2.3.4:1234"
	s.ServiceName = "myService"
	s.ServiceColor = "pink"
	s.ServiceDomain = []string{"my-domain.com"}
	s.OutboundHostname = "machine-123.my-company.com"
	s.BaseUrl = "/v1/docker-flow-proxy"
	s.ReconfigureBaseUrl = fmt.Sprintf("%s/reconfigure", s.BaseUrl)
	s.RemoveBaseUrl = fmt.Sprintf("%s/remove", s.BaseUrl)
	s.ReconfigureUrl = fmt.Sprintf(
		"%s?serviceName=%s&serviceColor=%s&servicePath=%s&serviceDomain=%s&outboundHostname=%s",
		s.ReconfigureBaseUrl,
		s.ServiceName,
		s.ServiceColor,
		strings.Join(s.sd.ServicePath, ","),
		strings.Join(s.ServiceDomain, ","),
		s.OutboundHostname,
	)
	s.ReqMode = "http"
	s.RemoveUrl = fmt.Sprintf("%s?serviceName=%s", s.RemoveBaseUrl, s.ServiceName)
	s.CertUrl = fmt.Sprintf("%s/cert?my-cert.pem", s.BaseUrl)
	s.CertsUrl = fmt.Sprintf("%s/certs", s.BaseUrl)
	s.ConfigUrl = "/v1/docker-flow-proxy/config"
	s.ResponseWriter = getResponseWriterMock()
	s.RequestReconfigure, _ = http.NewRequest("GET", s.ReconfigureUrl, nil)
	s.RequestRemove, _ = http.NewRequest("GET", s.RemoveUrl, nil)
	usersBasePath = "./test_configs/%s.txt"
	httpListenAndServe = func(addr string, handler http.Handler) error {
		return nil
	}
	serverImpl = Serve{
		BaseReconfigure: actions.BaseReconfigure{
			ConsulAddresses: []string{s.ConsulAddress},
			InstanceName:    s.InstanceName,
		},
	}
	actions.NewReconfigure = func(baseData actions.BaseReconfigure, serviceData proxy.Service, mode string) actions.Reconfigurable {
		return getReconfigureMock("")
	}
	logPrintfOrig := logPrintf
	defer func() { logPrintf = logPrintfOrig }()
	logPrintf = func(format string, v ...interface{}) {}
}

// Execute

func (s *ServerTestSuite) Test_Execute_InvokesHTTPListenAndServe() {
	serverImpl := Serve{
		IP:   "myIp",
		Port: "1234",
	}
	var actual string
	expected := fmt.Sprintf("%s:%s", serverImpl.IP, serverImpl.Port)
	httpListenAndServe = func(addr string, handler http.Handler) error {
		actual = addr
		return nil
	}

	serverImpl.Execute([]string{})
	time.Sleep(1 * time.Millisecond)

	s.Equal(expected, actual)
}

func (s *ServerTestSuite) Test_Execute_ReturnsError_WhenHTTPListenAndServeFails() {
	orig := httpListenAndServe
	defer func() {
		httpListenAndServe = orig
	}()
	httpListenAndServe = func(addr string, handler http.Handler) error {
		return fmt.Errorf("This is an error")
	}

	actual := serverImpl.Execute([]string{})

	s.Error(actual)
}

func (s *ServerTestSuite) Test_Execute_InvokesRunExecute() {
	orig := NewRun
	defer func() {
		NewRun = orig
	}()
	mockObj := getRunMock("")
	NewRun = func() Executable {
		return mockObj
	}

	serverImpl.Execute([]string{})

	mockObj.AssertCalled(s.T(), "Execute", []string{})
}

func (s *ServerTestSuite) Test_Execute_InvokesCertInit() {
	invoked := false
	err := serverImpl.Execute([]string{})
	certOrig := cert
	defer func() { cert = certOrig }()
	cert = CertMock{
		GetInitMock: func() error {
			invoked = true
			return nil
		},
	}
	serverImpl.Execute([]string{})

	s.NoError(err)
	s.True(invoked)
}

func (s *ServerTestSuite) Test_Execute_InvokesReloadAllServices() {
	mockObj := getReconfigureMock("")
	actions.NewReconfigure = func(baseData actions.BaseReconfigure, serviceData proxy.Service, mode string) actions.Reconfigurable {
		return mockObj
	}
	consulAddressesOrig := []string{s.ConsulAddress}
	defer func() {
		os.Unsetenv("CONSUL_ADDRESS")
		serverImpl.ConsulAddresses = consulAddressesOrig
	}()
	os.Setenv("CONSUL_ADDRESS", s.ConsulAddress)

	serverImpl.Execute([]string{})

	mockObj.AssertCalled(s.T(), "ReloadAllServices", []string{s.ConsulAddress}, s.InstanceName, "", "")
}

func (s *ServerTestSuite) Test_Execute_InvokesReloadAllServicesWithListenerAddress() {
	listenerAddress := "swarm-listener"
	mockObj := getReconfigureMock("")
	actions.NewReconfigure = func(baseData actions.BaseReconfigure, serviceData proxy.Service, mode string) actions.Reconfigurable {
		return mockObj
	}
	consulAddressesOrig := []string{s.ConsulAddress}
	defer func() {
		os.Unsetenv("CONSUL_ADDRESS")
		os.Unsetenv("LISTENER_ADDRESS")
		serverImpl.ConsulAddresses = consulAddressesOrig
	}()
	os.Setenv("CONSUL_ADDRESS", s.ConsulAddress)
	serverImpl.ListenerAddress = listenerAddress

	serverImpl.Execute([]string{})

	mockObj.AssertCalled(
		s.T(),
		"ReloadAllServices",
		[]string{s.ConsulAddress},
		s.InstanceName,
		"",
		fmt.Sprintf("http://%s:8080", listenerAddress),
	)
}

func (s *ServerTestSuite) Test_Execute_DoesNotInvokeReloadAllServices_WhenModeIsService() {
	serverImpl.Mode = "seRviCe"
	mockObj := getReconfigureMock("")
	actions.NewReconfigure = func(baseData actions.BaseReconfigure, serviceData proxy.Service, mode string) actions.Reconfigurable {
		return mockObj
	}

	serverImpl.Execute([]string{})

	mockObj.AssertNotCalled(s.T(), "ReloadAllServices", s.ConsulAddress, s.InstanceName, "")
}

func (s *ServerTestSuite) Test_Execute_DoesNotInvokeReloadAllServices_WhenModeIsSwarm() {
	serverImpl.Mode = "SWarM"
	mockObj := getReconfigureMock("")
	actions.NewReconfigure = func(baseData actions.BaseReconfigure, serviceData proxy.Service, mode string) actions.Reconfigurable {
		return mockObj
	}

	serverImpl.Execute([]string{})

	mockObj.AssertNotCalled(s.T(), "ReloadAllServices", s.ConsulAddress, s.InstanceName, "")
}

func (s *ServerTestSuite) Test_Execute_ReturnsError_WhenReloadAllServicesFails() {
	mockObj := getReconfigureMock("ReloadAllServices")
	mockObj.On("ReloadAllServices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(fmt.Errorf("This is an error"))
	actions.NewReconfigure = func(baseData actions.BaseReconfigure, serviceData proxy.Service, mode string) actions.Reconfigurable {
		return mockObj
	}

	actual := serverImpl.Execute([]string{})

	s.Error(actual)
}

func (s *ServerTestSuite) Test_Execute_SetsConsulAddressesToEmptySlice_WhenEnvVarIsNotset() {
	srv := Serve{}

	srv.Execute([]string{})

	s.Equal([]string{}, srv.ConsulAddresses)
}

func (s *ServerTestSuite) Test_Execute_SetsConsulAddresses() {
	expected := "http://my-consul"
	consulAddressesOrig := serverImpl.ConsulAddresses
	defer func() {
		os.Unsetenv("CONSUL_ADDRESS")
		serverImpl.ConsulAddresses = consulAddressesOrig
	}()
	os.Setenv("CONSUL_ADDRESS", expected)
	srv := Serve{}

	srv.Execute([]string{})

	s.Equal([]string{expected}, srv.ConsulAddresses)
}

func (s *ServerTestSuite) Test_Execute_SetsMultipleConsulAddresseses() {
	expected := []string{"http://my-consul-1", "http://my-consul-2"}
	consulAddressesOrig := serverImpl.ConsulAddresses
	defer func() {
		os.Unsetenv("CONSUL_ADDRESS")
		serverImpl.ConsulAddresses = consulAddressesOrig
	}()
	os.Setenv("CONSUL_ADDRESS", strings.Join(expected, ","))
	srv := Serve{}

	srv.Execute([]string{})

	s.Equal(expected, srv.ConsulAddresses)
}

func (s *ServerTestSuite) Test_Execute_AddsHttpToConsulAddresses() {
	expected := []string{"http://my-consul-1", "http://my-consul-2"}
	consulAddressesOrig := serverImpl.ConsulAddresses
	defer func() {
		os.Unsetenv("CONSUL_ADDRESS")
		serverImpl.ConsulAddresses = consulAddressesOrig
	}()
	os.Setenv("CONSUL_ADDRESS", "my-consul-1,my-consul-2")
	srv := Serve{}

	srv.Execute([]string{})

	s.Equal(expected, srv.ConsulAddresses)
}

// CertPutHandler

func (s *ServerTestSuite) Test_CertPutHandler_InvokesCertPut_WhenUrlIsCert() {
	invoked := false
	certOrig := cert
	defer func() { cert = certOrig }()
	cert = CertMock{
		PutMock: func(http.ResponseWriter, *http.Request) (string, error) {
			invoked = true
			return "", nil
		},
	}
	req, _ := http.NewRequest("PUT", s.CertUrl, nil)

	srv := Serve{}
	srv.CertPutHandler(s.ResponseWriter, req)

	s.Assert().True(invoked)
}

// CertsHandler

func (s *ServerTestSuite) Test_CertsHandler_InvokesCertGetAll_WhenUrlIsCerts() {
	invoked := false
	certOrig := cert
	defer func() { cert = certOrig }()
	cert = CertMock{
		GetAllMock: func(http.ResponseWriter, *http.Request) (server.CertResponse, error) {
			invoked = true
			return server.CertResponse{}, nil
		},
	}
	req, _ := http.NewRequest("GET", s.CertsUrl, nil)

	srv := Serve{}
	srv.CertsHandler(s.ResponseWriter, req)

	s.Assert().True(invoked)
}

// ServeHTTP > Config

func (s *ServerTestSuite) Test_ConfigHandler_SetsContentTypeToText_WhenUrlIsConfig() {
	var actual string
	httpWriterSetContentType = func(w http.ResponseWriter, value string) {
		actual = value
	}
	req, _ := http.NewRequest("GET", s.ConfigUrl, nil)

	srv := Serve{}
	srv.ConfigHandler(s.ResponseWriter, req)

	s.Equal("text/html", actual)
}

func (s *ServerTestSuite) Test_ConfigHandler_ReturnsConfig_WhenUrlIsConfig() {
	expected := "some text"
	readFileOrig := proxy.ReadFile
	defer func() { proxy.ReadFile = readFileOrig }()
	proxy.ReadFile = func(filename string) ([]byte, error) {
		return []byte(expected), nil
	}

	req, _ := http.NewRequest("GET", s.ConfigUrl, nil)
	srv := Serve{}
	srv.ConfigHandler(s.ResponseWriter, req)

	s.ResponseWriter.AssertCalled(s.T(), "Write", []byte(expected))
}

func (s *ServerTestSuite) Test_ConfigHandler_ReturnsStatus500_WhenReadFileFails() {
	readFileOrig := readFile
	defer func() { readFile = readFileOrig }()
	readFile = func(filename string) ([]byte, error) {
		return []byte(""), fmt.Errorf("This is an error")
	}

	req, _ := http.NewRequest("GET", s.ConfigUrl, nil)
	srv := Serve{}
	srv.ConfigHandler(s.ResponseWriter, req)

	s.ResponseWriter.AssertCalled(s.T(), "WriteHeader", 500)
}

// Suite

// TODO: Review whether everything is needed
func TestServerUnitTestSuite(t *testing.T) {
	s := new(ServerTestSuite)
	logPrintf = func(format string, v ...interface{}) {}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actualPath := r.URL.Path
		if r.Method == "GET" {
			switch actualPath {
			case "/v1/docker-flow-proxy/reconfigure":
				if strings.EqualFold(r.URL.Query().Get("returnError"), "true") {
					w.WriteHeader(http.StatusInternalServerError)
				} else {
					w.WriteHeader(http.StatusOK)
					w.Header().Set("Content-Type", "application/json")
				}
			case "/v1/docker-flow-proxy/remove":
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer func() { s.Server.Close() }()
	addr := strings.Replace(s.Server.URL, "http://", "", -1)
	s.DnsIps = []string{strings.Split(addr, ":")[0]}

	lookupHostOrig := lookupHost
	defer func() { lookupHost = lookupHostOrig }()
	lookupHost = func(host string) (addrs []string, err error) {
		return s.DnsIps, nil
	}
	sd := proxy.ServiceDest{
		Port: strings.Split(addr, ":")[1],
	}
	s.ServiceDest = []proxy.ServiceDest{sd}

	suite.Run(t, s)
}

// Mock

type ServerMock struct {
	mock.Mock
}

func (m *ServerMock) Execute(args []string) error {
	params := m.Called(args)
	return params.Error(0)
}

func (m *ServerMock) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	m.Called(w, req)
}

func getServerMock() *ServerMock {
	mockObj := new(ServerMock)
	mockObj.On("Execute", mock.Anything).Return(nil)
	mockObj.On("ServeHTTP", mock.Anything, mock.Anything)
	return mockObj
}

type ResponseWriterMock struct {
	mock.Mock
}

func (m *ResponseWriterMock) Header() http.Header {
	m.Called()
	return make(map[string][]string)
}

func (m *ResponseWriterMock) Write(data []byte) (int, error) {
	params := m.Called(data)
	return params.Int(0), params.Error(1)
}

func (m *ResponseWriterMock) WriteHeader(header int) {
	m.Called(header)
}

func getResponseWriterMock() *ResponseWriterMock {
	mockObj := new(ResponseWriterMock)
	mockObj.On("Header").Return(nil)
	mockObj.On("Write", mock.Anything).Return(0, nil)
	mockObj.On("WriteHeader", mock.Anything)
	return mockObj
}

type CertMock struct {
	PutMock     func(http.ResponseWriter, *http.Request) (string, error)
	PutCertMock func(certName string, certContent []byte) (string, error)
	GetAllMock  func(w http.ResponseWriter, req *http.Request) (server.CertResponse, error)
	GetInitMock func() error
}

func (m CertMock) Put(w http.ResponseWriter, req *http.Request) (string, error) {
	return m.PutMock(w, req)
}

func (m CertMock) PutCert(certName string, certContent []byte) (string, error) {
	return m.PutCertMock(certName, certContent)
}

func (m CertMock) GetAll(w http.ResponseWriter, req *http.Request) (server.CertResponse, error) {
	return m.GetAllMock(w, req)
}

func (m CertMock) Init() error {
	return m.GetInitMock()
}

type ReloadMock struct {
	ExecuteMock func(recreate bool, listenerAddr string) error
}

func (m ReloadMock) Execute(recreate bool, listenerAddr string) error {
	return m.ExecuteMock(recreate, listenerAddr)
}

type RunMock struct {
	mock.Mock
}

func (m *RunMock) Execute(args []string) error {
	params := m.Called(args)
	return params.Error(0)
}

func getRunMock(skipMethod string) *ReconfigureMock {
	mockObj := new(ReconfigureMock)
	if skipMethod != "Execute" {
		mockObj.On("Execute", mock.Anything).Return(nil)
	}
	return mockObj
}

type ReconfigureMock struct {
	mock.Mock
}

func (m *ReconfigureMock) Execute(args []string) error {
	params := m.Called(args)
	return params.Error(0)
}

func (m *ReconfigureMock) GetData() (actions.BaseReconfigure, proxy.Service) {
	m.Called()
	return actions.BaseReconfigure{}, proxy.Service{}
}

func (m *ReconfigureMock) ReloadAllServices(addresses []string, instanceName, mode, listenerAddress string) error {
	params := m.Called(addresses, instanceName, mode, listenerAddress)
	return params.Error(0)
}

func (m *ReconfigureMock) GetTemplates(sr *proxy.Service) (front, back string, err error) {
	params := m.Called(sr)
	return params.String(0), params.String(1), params.Error(2)
}

func getReconfigureMock(skipMethod string) *ReconfigureMock {
	mockObj := new(ReconfigureMock)
	if skipMethod != "Execute" {
		mockObj.On("Execute", mock.Anything).Return(nil)
	}
	if skipMethod != "GetData" {
		mockObj.On("GetData", mock.Anything, mock.Anything).Return(nil)
	}
	if skipMethod != "ReloadAllServices" {
		mockObj.On("ReloadAllServices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	}
	if skipMethod != "GetTemplates" {
		mockObj.On("GetTemplates", mock.Anything).Return("", "", nil)
	}
	return mockObj
}

