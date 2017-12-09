const UPDATE_INTERVAL_SEC = 2

$(document).ready(() => {
	setInterval(updateName, UPDATE_INTERVAL_SEC*1000);
});

function updateName() {
	$.ajax({
		url: '/name',
		type: 'GET',
		dataType: 'json',
	})
	.done(function(data) {
		if (data == null || data['name'] == null) return;
		displayName(data['name']);
	})
	.fail(function(data) {
		console.log('Error updating name');
	});
}

function displayName(name) {
	$('#peer-id').html(name);
}

function submitInput(e, url, formId, inputId) {
	e.preventDefault();

	if ($(inputId).val() == "") {
		// TODO display stuff maybe
		alert('Field required')
		return
	}

	$.ajax({
		type: "POST",
		url: url,
		data: $(formId).serialize(),
    })
    .done(function() {
		$(inputId).val("");
	})
}

function prependMessage(listId, origin, msg) {
	author = $('<h4>').addClass('uk-text-success uk-text-left uk-padding-remove uk-margin-remove uk-width-1-5@m').html(origin);
	content = $('<p>').addClass('uk-text-right uk-padding-remove uk-margin-remove uk-width-4-5@m').html(msg);

	message = $('<div>').addClass('uk-flex').append(author).append(content);

	elem = $('<li>').addClass('uk-padding-remove uk-margin-remove');
	elem.append(message);
	$('#' + listId).prepend(elem);
}
