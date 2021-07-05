from mininet.topo import Topo
from mininet.net import Mininet
from mininet.node import Node
from mininet.log import setLogLevel, info
from mininet.cli import CLI


class LinuxRouter(Node):
    "A Node with IP forwarding enabled."

    def config(self, **params):
         super(LinuxRouter, self).config(**params)
         # Enable forwarding on the router
         self.cmd('sysctl net.ipv4.ip_forward=1')

    def terminate(self):
        self.cmd('sysctl net.ipv4.ip_forward=0')
        super(LinuxRouter, self).terminate()


class NetworkTopo(Topo):

    def build(self, **_opts):

        defaultIP = '10.0.1.4/24'  # IP address for r1-eth1
        router = self.addNode('r1', cls=LinuxRouter, ip=defaultIP)

        s1, s2 = [self.addSwitch(s) for s in ('s1', 's2')]

        self.addLink(s1, router, intfName2='r1-eth1', params2={'ip':       defaultIP})  # for clarity
        self.addLink(s2, router, intfName2='r1-eth2',
                 params2={'ip': '10.0.2.4/24'})

        h1 = self.addHost('h1', ip='10.0.1.1/24', defaultRoute='via 10.0.1.4')
        h2 = self.addHost('h2', ip='10.0.2.1/24', defaultRoute='via 10.0.2.4')

        for h, s in [(h1, s1), (h2, s2)]:
            self.addLink(h, s)


def run():
    topo = NetworkTopo()
    net = Mininet(topo=topo)  # controller is used by s1-s3
    net.start()
    info('*** Routing Table on Router:\n')
    info(net['r1'].cmd('route'))
    CLI(net)
    net.stop()


if __name__ == '__main__':
    setLogLevel('info')
    run()
