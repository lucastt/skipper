package eskipfile

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zalando/skipper/eskip"
	"github.com/zalando/skipper/net"
	"github.com/zalando/skipper/routing"

	log "github.com/sirupsen/logrus"
)

type remoteEskipFile struct {
	once            sync.Once
	preloaded       bool
	remotePath      string
	localPath       string
	eskipFileClient *WatchClient
	threshold       int
	verbose         bool
	http            *net.Client
}

type RemoteWatchOptions struct {
	// URL of the route file
	RemoteFile string

	// Verbose mode for the dataClient
	Verbose bool

	// Amount of route changes that will trigger logs after route updates
	Threshold int

	// It does an initial download and parsing of remote routes, and makes RemoteWatch to return an error
	FailOnStartup bool

	// HTTPTimeout is the generic timeout for any phase of a single HTTP request to RemoteFile.
	HTTPTimeout time.Duration
}

// RemoteWatch creates a route configuration client with (remote) file watching. Watch doesn't follow file system nodes,
// it always reads (or re-downloads) from the file identified by the initially provided file name.
func RemoteWatch(o *RemoteWatchOptions) (routing.DataClient, error) {
	if !isFileRemote(o.RemoteFile) {
		return Watch(o.RemoteFile), nil
	}

	tempFilename, err := os.CreateTemp("", "routes")

	if err != nil {
		return nil, err
	}

	dataClient := &remoteEskipFile{
		once:       sync.Once{},
		remotePath: o.RemoteFile,
		localPath:  tempFilename.Name(),
		threshold:  o.Threshold,
		verbose:    o.Verbose,
		http:       net.NewClient(net.Options{Timeout: o.HTTPTimeout}),
	}

	if o.FailOnStartup {
		err = dataClient.DownloadRemoteFile()

		if err != nil {
			return nil, err
		}
	} else {
		f, err := os.OpenFile(tempFilename.Name(), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err == nil {
			err = f.Close()
		}

		if err != nil {
			return nil, err
		}
		dataClient.preloaded = true
	}

	dataClient.eskipFileClient = Watch(tempFilename.Name())

	return dataClient, nil
}

// LoadAll returns the parsed route definitions found in the file.
func (client *remoteEskipFile) LoadAll() ([]*eskip.Route, error) {
	var err error = nil

	if client.preloaded {
		client.preloaded = false
	} else {
		err = client.DownloadRemoteFile()
	}

	if err != nil {
		log.Errorf("LoadAll from remote %s failed. Continue using the last loaded routes", client.remotePath)
		return nil, err
	}

	if client.verbose {
		log.Infof("New routes file %s was downloaded", client.remotePath)
	}

	return client.eskipFileClient.LoadAll()
}

// LoadUpdate returns differential updates when a remote file has changed.
func (client *remoteEskipFile) LoadUpdate() ([]*eskip.Route, []string, error) {
	err := client.DownloadRemoteFile()

	if err != nil {
		log.Errorf("LoadUpdate from remote %s failed. Trying to LoadAll", client.remotePath)
		return nil, nil, err
	}

	newRoutes, deletedRoutes, err := client.eskipFileClient.LoadUpdate()
	if err == nil {
		if client.verbose {
			log.Infof("New routes were loaded. New: %d; deleted: %d", len(newRoutes), len(deletedRoutes))

			if client.threshold > 0 {
				if len(newRoutes)+len(deletedRoutes) > client.threshold {
					log.Warnf("Significant amount of routes was updated. New: %d; deleted: %d", len(newRoutes), len(deletedRoutes))
				}
			}
		}
	} else {
		log.Errorf("RemoteEskipFile LoadUpdate %s failed. Skipper continues to serve the last successfully updated routes. Error: %s",
			client.remotePath, err)
	}

	return newRoutes, deletedRoutes, err
}

func (client *remoteEskipFile) Close() {
	client.once.Do(func() {
		client.http.Close()
		client.eskipFileClient.Close()
	})
}

func isFileRemote(remotePath string) bool {
	return strings.HasPrefix(remotePath, "http://") || strings.HasPrefix(remotePath, "https://")
}

func (client *remoteEskipFile) DownloadRemoteFile() error {
	data, err := client.getRemoteData()
	if err != nil {
		return err
	}
	defer data.Close()

	out, err := os.OpenFile(client.localPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, data); err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
}

func (client *remoteEskipFile) getRemoteData() (io.ReadCloser, error) {
	resp, err := client.http.Get(client.remotePath)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("download file failed")
	}

	return resp.Body, nil
}
