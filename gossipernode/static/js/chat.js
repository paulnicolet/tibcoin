$(document).ready(() => {
	$('#message-form').submit(e => submitInput(e, "/message", "#message-form", "#message"));

	setInterval(updateChatPage, UPDATE_INTERVAL_SEC*1000);
});

function updateChatPage() {
	updatePeers();
	updateChat();
}

function updatePeers() {
	$.ajax({
		url: '/peers',
		type: 'GET',
		dataType: 'json',
	})
	.done(function(data) {
		if (data == null || data['peers'] == null) return;
		displayPeers(data['peers']);
	})
	.fail(function(data) {
		console.log('Error updating peers');
	});
}

function updateChat() {
	$.ajax({
		url: '/chat',
		type: 'GET',
		dataType: 'json',
	})
	.done(function(data) {
		if (data == null || data['chat'] == null) return;
		displayChat(data['chat']);
	})
	.fail(function(data) {
		console.log('Error updating chat');
	});
}

function displayChat(chat) {
	$('#chat-list').empty();

	chat.forEach(elem => {
		var origin = elem['Origin']
		var id = elem['ID']
		var msg = elem['Text']

		prependMessage('chat-list', origin, msg);
	});
}

function displayPeers(peers) {
	$('#peers-list').empty();
	peers.forEach(function(peer) {
		elem = $('<li>').text(peer);
		$('#peers-list').append(elem);
	});
}
