const FILE_URL = '/share-file';
const SEARCH_URL = '/search';
const MATCHES_URL = '/matches';
const DOWNLOAD_URL = '/download';
const MACTHES_UPDATE_DELAY = 1 * 1000;

$(document).ready(() => {
	// Submit file on click
	$('#file-submit').on('click', function() {
		if ($('#file').prop('files').length > 0) {
			submitFile($('#file').prop('files')[0]);
		} else {
			window.alert('Please choose a file before submitting');
		}
	});

	// Submit keywords
	$('#keywords-submit').on('click', function() {
		if($('#keywords').val().length > 0) {
			submitKeywords($('#keywords').val());
		} else {
			window.alert('Please type at least one keyword');
		}
	});

	// Update matches
	setInterval(function() {
		updateMatches();
	}, MACTHES_UPDATE_DELAY)
});

function updateMatches() {
	$.ajax({
		type: "GET",
		url: MATCHES_URL
    })
    .done(function(data) {
    	data = JSON.parse(data);
    	if (data['matches'] != null) {
    		displayMatches(data['matches']);
    	}
    })
    .fail(function() {
    	console.log('Error updating matches');
    });
}

function displayMatches(matches) {
	$('#matches-list').empty();
	console.log(matches);

	matches.forEach(function(match) {
		var elem = $('<li>').text(match);
		elem.on('click', function(e) {
			downloadFile($(e.target).html());
		});

		$('#matches-list').append(elem);
	});
}

function downloadFile(filename) {
	$.ajax({
		type: "POST",
		url: DOWNLOAD_URL,
		data: {'filename': filename},
    })
    .done(function() {
    	UIkit.notification('Download started 🎉', {status: 'success', pos: 'top-right'});
	});
}

function submitKeywords(keywords) {
	console.log(keywords);

	$.ajax({
		type: "POST",
		url: SEARCH_URL,
		data: {'keywords': keywords},
    })
    .done(function() {
    	UIkit.notification('Search started 🎉', {status: 'success', pos: 'top-right'});
	});
}


function submitFile(file) {
	console.log(file.name);
	$.ajax({
		type: "POST",
		url: FILE_URL,
		data: {'filename': file.name},
    })
    .fail(function() {
    	UIkit.notification('File not found in shared folder', {status: 'warning', pos: 'top-right'});
    })
    .done(function() {
    	UIkit.notification('File shared 🎉', {status: 'success', pos: 'top-right'});
	});
}
