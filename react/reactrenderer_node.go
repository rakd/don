package react

import (
	"bufio"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

type NodeJSRenderer struct {
	count int
	procs chan *nodeProcess
}

func NewNodeJSRenderer(count int) *NodeJSRenderer {
	procs := make(chan *nodeProcess, count)
	for i := 0; i < count; i++ {
		procs <- nil
	}

	return &NodeJSRenderer{
		count: count,
		procs: procs,
	}
}

func (r *NodeJSRenderer) Render(code, inputURL, inputJSON string) (string, error) {
	var html string

	if err := r.withProcess(code, func(addr string) error {
		res, err := http.Post("http://"+addr+inputURL, "application/json", strings.NewReader(inputJSON))
		if err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.Render")
		}
		defer res.Body.Close()

		d, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.Render")
		}

		html = string(d)

		return nil
	}); err != nil {
		return "", errors.Wrap(err, "ReactRendererDuktape.Render")
	}

	return html, nil
}

type nodeProcess struct {
	code string
	proc *exec.Cmd
	addr string
}

func (r *NodeJSRenderer) withProcess(code string, fn func(addr string) error) error {
	proc := <-r.procs
	defer func() {
		r.procs <- proc
	}()

	if proc != nil && proc.code != code {
		if err := proc.proc.Process.Kill(); err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.withProcess")
		}

		proc = nil
	}

	if proc == nil {
		fd, err := ioutil.TempFile("", "react-render-server")
		if err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.withProcess")
		}
		defer fd.Close()

		if _, err := io.Copy(fd, strings.NewReader(code+"\n"+reactRendererNodeServer)); err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.withProcess")
		}

		c := exec.Command("node", fd.Name())

		out, err := c.StdoutPipe()
		if err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.withProcess")
		}

		rd := bufio.NewReader(out)

		if err := c.Start(); err != nil {
			return errors.Wrap(err, "ReactRendererDuktape.withProcess")
		}

		s, err := rd.ReadString('\n')
		if err != nil {
			c.Process.Kill()
			return errors.Wrap(err, "ReactRendererDuktape.withProcess")
		}

		proc = &nodeProcess{code: code, addr: strings.TrimSpace(s), proc: c}
	}

	if err := fn(proc.addr); err != nil {
		proc.proc.Process.Kill()
		proc = nil
		return errors.Wrap(err, "ReactRendererDuktape.withProcess")
	}

	return nil
}

const reactRendererNodeServer = `
const { createServer } = require('http');

createServer((req, res) => {
  const chunks = [];

  req.on('data', chunk => chunks.push(chunk)).on('end', () => {
    const body = Buffer.concat(chunks);

    let html = null;
    try {
      html = module.exports(req.url, body.toString('utf8'));
    } catch (e) {
      res.writeHead(500);
      res.end(e + '');
      return;
    }

    res.writeHead(200);
    res.end(html);
  });
}).listen(0, function() {
  console.log('127.0.0.1:' + this.address().port);
});
`
