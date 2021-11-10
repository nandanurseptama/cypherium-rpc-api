require('dotenv').config()
module.exports = function(req, res, next){
    console.log('app auth');
    console.log(req.headers);
    console.log(process.env.AUTHORIZATION);
    if(req.headers.authorization === process.env.AUTHORIZATION){
        next();
    } else{
        res.status(401).json(null);
    }
};