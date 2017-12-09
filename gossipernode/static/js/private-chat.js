var privateChatUpdater;

$(document).ready(() => {
	$('#private-chat-form').submit(e => submitInput(e, "/private", "#private-chat-form", "#private-chat-input"));

	setInterval(getDestinations, UPDATE_INTERVAL_SEC*1000);
});


function getDestinations() {
	$.ajax({
			url: '/destinations',
			type: 'GET',
			dataType: 'json'
		})
	.done(function(data) {
		if (data["destinations"] == null) return;

		$('#destinations-list').empty();

		data["destinations"].forEach(function(dest) {
			link = $('<a>').html(dest).attr('dest', dest);
			elem = $('<li>').addClass('uk-text-center').attr('dest', dest);
			elem.append(link);

			elem.click(function(event) {
				setupPrivateChat($(event.target).attr('dest'));
			});

			$('#destinations-list').append(elem);
		});

	})
	.fail(function() {
		console.log("Error getting destinations.");
	});
}

function setupPrivateChat(dest) {
	// Show modal
	UIkit.modal($('#private-chat-modal')).show();
	$('#private-chat-dest').html(dest);
	$('#private-chat-content').empty();

	// Start polling chat for given destination
	privateChatUpdater = setInterval(updatePrivateChat, UPDATE_INTERVAL_SEC*1000);
	$('#private-chat-dest-input').attr('value', dest);

	// Upon modal close, stop chat updates
	$('#private-chat-modal').on('beforehide', function(event) {
		clearInterval(privateChatUpdater);
	});
}

function updatePrivateChat() {
	$.ajax({
		url: '/private-chat',
		type: 'GET',
		dataType: 'json',
		data: {dest: $('#private-chat-dest-input').attr('value')}
	})
	.done(function(data) {
		if (data == null || data["private-chat"] == null) return;

		$('#private-chat-content').empty();

		data['private-chat'].forEach(function(elem) {
			origin = elem['Origin'];
			msg = elem['Text'];

			prependMessage('private-chat-content', origin, msg);
		});
	})
	.fail(function() {
		console.log("Error getting private chat.");
	});
}
