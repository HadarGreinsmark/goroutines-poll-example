var net = require('net');

var server = net.createServer(function(socket) {
	socket.on('data', function(data) {
		var buf = data.toString();
		console.log("received:", buf)
		socket.write(buf.toUpperCase(), function() {
			socket.end()
		});
	});
})

server.listen(7777);